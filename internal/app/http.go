package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	urlpath "path"
	"strings"
)

type userIDKey struct{}

const confirmationHeader = "X-Palpanel-Confirm"
const sessionCookieName = "palpanel_session"
const maxJSONRequestBodyBytes int64 = 1 << 20

var errConfirmationRequired = errors.New("confirmation required for this operation")
var errCrossOriginUnsafeRequest = errors.New("cross-origin unsafe request forbidden")
var errJSONRequestBodyTooLarge = errors.New("json request body is too large")
var errJSONTrailingData = errors.New("json request body must contain only one JSON value")
var errJSONDuplicateField = errors.New("json request body contains a duplicate object field")
var errUnexpectedRequestBody = errors.New("request body must be empty")

func (a *App) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
			return
		}
		a.mu.RLock()
		userID, ok := a.sessions[cookie.Value]
		a.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey{}, userID)
		next(w, r.WithContext(ctx))
	}
}

func (a *App) createSession(w http.ResponseWriter, r *http.Request, userID int64) {
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.mu.Lock()
	a.sessions[token] = userID
	a.mu.Unlock()
	setSessionCookie(w, r, &http.Cookie{
		Value: token,
	})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, cookie *http.Cookie) {
	cookie.Name = sessionCookieName
	cookie.Path = "/"
	cookie.HttpOnly = true
	cookie.SameSite = http.SameSiteLaxMode
	cookie.Secure = strings.EqualFold(requestScheme(r), "https")
	http.SetCookie(w, cookie)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	data, ok := readLimitedJSONRequestBody(w, r)
	if !ok {
		return false
	}
	return decodeJSONData(w, data, out)
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	data, ok := readLimitedJSONRequestBody(w, r)
	if !ok {
		return false
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	return decodeJSONData(w, data, out)
}

func readLimitedJSONRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONDecodeError(w, err)
		return nil, false
	}
	return data, true
}

func decodeJSONData(w http.ResponseWriter, data []byte, out any) bool {
	if err := validateJSONRequestBody(data); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}
	return true
}

func requireNoRequestBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	defer r.Body.Close()
	if r.ContentLength == 0 {
		return true
	}
	if r.ContentLength > 0 {
		writeError(w, http.StatusBadRequest, errUnexpectedRequestBody)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	var probe [1]byte
	n, err := r.Body.Read(probe[:])
	if n > 0 {
		writeError(w, http.StatusBadRequest, errUnexpectedRequestBody)
		return false
	}
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeError(w, http.StatusBadRequest, err)
	return false
}

func validateJSONRequestBody(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errJSONTrailingData
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("json object field name must be a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("%w: %s", errJSONDuplicateField, key)
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		endToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if endToken != json.Delim('}') {
			return errors.New("json object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		endToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if endToken != json.Delim(']') {
			return errors.New("json array is not closed")
		}
	default:
		return errors.New("unexpected json delimiter")
	}
	return nil
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeError(w, http.StatusRequestEntityTooLarge, errJSONRequestBodyTooLarge)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": redactSensitive(err.Error())})
}

func requireConfirmation(w http.ResponseWriter, r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(confirmationHeader)), "true") {
		return true
	}
	writeError(w, http.StatusPreconditionRequired, errConfirmationRequired)
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if !unsafeRequestOriginAllowed(r) {
			writeError(w, http.StatusForbidden, errCrossOriginUnsafeRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func unsafeRequestOriginAllowed(r *http.Request) bool {
	if !isUnsafeMethod(r.Method) {
		return true
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	scheme := requestScheme(r)
	return strings.EqualFold(parsed.Scheme, scheme) &&
		canonicalOriginHost(parsed.Host, scheme) == canonicalOriginHost(r.Host, scheme)
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func requestScheme(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		if index := strings.Index(forwarded, ","); index >= 0 {
			forwarded = forwarded[:index]
		}
		if forwarded = strings.ToLower(strings.TrimSpace(forwarded)); forwarded != "" {
			return forwarded
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func canonicalOriginHost(rawHost, scheme string) string {
	rawHost = strings.TrimSpace(rawHost)
	host, port, err := net.SplitHostPort(rawHost)
	if err != nil {
		return strings.ToLower(rawHost)
	}
	host = strings.ToLower(host)
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return host
	}
	return strings.ToLower(net.JoinHostPort(host, port))
}

func frontendHandler(staticFS fs.FS, fallbackIndex []byte) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(urlpath.Clean("/"+r.URL.Path), "/")
		if name == "." {
			name = ""
		}
		if name == "" {
			serveFrontendIndex(w, r, staticFS, fallbackIndex)
			return
		}
		if info, err := fs.Stat(staticFS, name); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if urlpath.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		serveFrontendIndex(w, r, staticFS, fallbackIndex)
	})
}

func serveFrontendIndex(w http.ResponseWriter, r *http.Request, staticFS fs.FS, fallbackIndex []byte) {
	data, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		data = fallbackIndex
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
