package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFrontendRoutesServeIndexAndKeepAssetsStrict(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	for _, path := range []string{"/", "/dashboard"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, resp.StatusCode)
		}
		if !strings.Contains(string(body), "PalPanel Lite") {
			t.Fatalf("GET %s did not serve frontend index: %s", path, string(body))
		}
	}

	resp, err := http.Get(server.URL + "/assets/missing.js")
	if err != nil {
		t.Fatalf("GET missing asset error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", resp.StatusCode)
	}
}

func TestCurrentServerStatusDetectsPalServerFromEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, palServerBinaryName()), []byte("test"), 0o755); err != nil {
		t.Fatalf("write PalServer binary: %v", err)
	}
	t.Setenv("PALPANEL_PAL_SERVER_PATH", dir)

	panel := &App{}
	status := panel.currentServerStatus(settingsPayload{})
	if status.Configured {
		t.Fatalf("detected path should not mark setting as configured")
	}
	if status.PalServerPath != dir {
		t.Fatalf("PalServerPath = %q, want %q", status.PalServerPath, dir)
	}
	if !status.PalServerExists {
		t.Fatalf("PalServerExists = false")
	}
	if status.PalServerBinary != filepath.Join(dir, palServerBinaryName()) {
		t.Fatalf("PalServerBinary = %q", status.PalServerBinary)
	}
}

func palServerBinaryName() string {
	if runtime.GOOS == "windows" {
		return "PalServer.exe"
	}
	return "PalServer.sh"
}
