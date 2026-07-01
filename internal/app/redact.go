package app

import "regexp"

const redactedValue = "[REDACTED]"

var sensitiveRedactors = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{
		pattern:     regexp.MustCompile(`(?i)(authorization\s*:\s*)[^\r\n]+`),
		replacement: `${1}` + redactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=._~-]{6,}`),
		replacement: `Basic ` + redactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(https?://[^/\s:@]+:)[^/\s@]+(@)`),
		replacement: `${1}` + redactedValue + `${2}`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(AdminPassword|ServerPassword)\s*=\s*"[^"\r\n]*"`),
		replacement: `${1}="` + redactedValue + `"`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(AdminPassword|ServerPassword)\s*=\s*[^,\s)\r\n]+`),
		replacement: `${1}=` + redactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(["']?(?:password|admin_password|server_password|rest_api_password)["']?\s*:\s*)"[^"\r\n]*"`),
		replacement: `${1}"` + redactedValue + `"`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(["']?(?:password|admin_password|server_password|rest_api_password)["']?\s*:\s*)'[^'\r\n]*'`),
		replacement: `${1}'` + redactedValue + `'`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(["']?(?:password|admin_password|server_password|rest_api_password)["']?\s*:\s*)[^"',\s}\]\r\n]+`),
		replacement: `${1}` + redactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(rest_api_password|admin_password|server_password|password)\s*=\s*("[^"\r\n]*"|'[^'\r\n]*'|[^&\s,)\r\n]+)`),
		replacement: `${1}=` + redactedValue,
	},
}

func redactSensitive(text string) string {
	for _, redactor := range sensitiveRedactors {
		text = redactor.pattern.ReplaceAllString(text, redactor.replacement)
	}
	return text
}
