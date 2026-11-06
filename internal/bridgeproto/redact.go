package bridgeproto

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Redacted replaces every secret value.
const Redacted = "[redacted]"

var (
	bearerPattern = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
	// secretFieldPattern matches a secret-named key followed by ':' or '=' and
	// its value in JSON, JavaScript object, query-string and log forms.
	secretFieldPattern = regexp.MustCompile(`(?i)(\b[A-Za-z0-9_-]*(?:access|refresh|session|password|secret|token|uid|captcha|human[-_]?verification)[A-Za-z0-9_-]*['"]?\s*[:=]\s*)('[^']*'|"[^"]*"|[^\s,;&}\]]+)`)
	// secretWordPattern matches "AccessToken <value>"-style log lines.
	secretWordPattern = regexp.MustCompile(`(?i)\b((?:access|refresh|session)[_-]?token\s+)[^\s,;:=]+`)
	// urlUserinfoPattern matches the userinfo of a URL (RFC 3986, section 3.2.1).
	urlUserinfoPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	nonAlphanumeric    = regexp.MustCompile(`[^A-Za-z0-9]`)
)

// RedactText masks bearer tokens, secret-named fields and URL userinfo in
// free text such as subprocess output.
func RedactText(text string) string {
	text = bearerPattern.ReplaceAllString(text, "${1}"+Redacted)
	text = secretFieldPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := secretFieldPattern.FindStringSubmatch(match)
		value := parts[2]
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
			return parts[1] + string(value[0]) + Redacted + string(value[0])
		}
		return parts[1] + Redacted
	})
	text = secretWordPattern.ReplaceAllString(text, "${1}"+Redacted)
	return urlUserinfoPattern.ReplaceAllString(text, "${1}"+Redacted+"@")
}

// exactSensitiveKeys and secretLocationKeys mirror proton-drive-cli
// src/utils/redaction.ts (keys lower-cased, non-alphanumerics removed).
var (
	exactSensitiveKeys = map[string]bool{
		"accesstoken": true, "authorization": true, "armoredkey": true,
		"captchatoken": true, "clientephemeral": true, "clientproof": true,
		"humanverificationtoken": true, "privatekey": true, "refreshtoken": true,
		"serverproof": true, "srpsession": true, "uid": true,
		"xpmhumanverificationtoken": true, "xpmuid": true,
	}
	secretLocationKeys = map[string]bool{"keypasswordprovider": true, "keypasswordhost": true}
)

// IsSensitiveKey reports whether a JSON key holds a secret, by the rules of
// proton-drive-cli's redactSensitive.
func IsSensitiveKey(key string) bool {
	normalized := strings.ToLower(nonAlphanumeric.ReplaceAllString(key, ""))
	switch {
	case exactSensitiveKeys[normalized]:
		return true
	case secretLocationKeys[normalized]:
		return false
	case strings.Contains(normalized, "password"), strings.Contains(normalized, "secret"),
		strings.Contains(normalized, "privatekey"), strings.Contains(normalized, "armoredkey"):
		return true
	default:
		return strings.HasSuffix(normalized, "token") && normalized != "tokentype"
	}
}

// Truncate shortens s to limit runes, ending with an ellipsis. It counts
// runes, not bytes, so the result is always valid UTF-8.
func Truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}
