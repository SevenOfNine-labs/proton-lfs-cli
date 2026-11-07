package bridgeproto

import (
	"strings"
	"testing"
)

func TestIsSensitiveKeyMirrorsRedactSensitive(t *testing.T) {
	cases := map[string]bool{
		"accessToken":     true,
		"refresh_token":   true,
		"x-pm-uid":        true,
		"uid":             true,
		"privateKey":      true,
		"armoredKey":      true,
		"clientProof":     true,
		"keyPassword":     true,
		"credentialStore": false,
		"credentialHost":  false,
		"tokenType":       false,
		"errorCode":       false,
		"state":           false,
	}
	for key, want := range cases {
		if got := IsSensitiveKey(key); got != want {
			t.Errorf("IsSensitiveKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestFilterEnvKeepsOnlyAllowlistedVariables(t *testing.T) {
	got := FilterEnv([]string{"PATH=/bin", "GCM_CREDENTIAL_STORE=secretservice", "PROTON_KEY_PASSWORD=x", "AWS_SECRET_ACCESS_KEY=y", "LC_ALL=C", "GIT_CONFIG_SYSTEM=/etc/gitconfig", "GNUPGHOME=/g", "DISPLAY=:0", "SSH_AUTH_SOCK=/s"})
	want := "PATH=/bin GCM_CREDENTIAL_STORE=secretservice LC_ALL=C GIT_CONFIG_SYSTEM=/etc/gitconfig GNUPGHOME=/g DISPLAY=:0"
	if strings.Join(got, " ") != want {
		t.Fatalf("FilterEnv() = %v, want %s", got, want)
	}
}

func TestRedactTextMasksSecretsKeepsContext(t *testing.T) {
	in := `Authorization: Bearer abc.def {"accessToken":"at-1"} https://uid-1@host.example/path`
	out := RedactText(in)
	for _, secret := range []string{"abc.def", "at-1", "uid-1"} {
		if strings.Contains(out, secret) {
			t.Fatalf("RedactText() leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "host.example/path") {
		t.Fatalf("RedactText() removed the URL host: %s", out)
	}
}

func TestMatchesAllowlistIgnoresCaseOnWindows(t *testing.T) {
	for _, key := range []string{"Path", "ComSpec", "windir", "SystemRoot"} {
		if !matchesAllowlist(key, true) {
			t.Errorf("matchesAllowlist(%q, windows) = false, want true", key)
		}
		if matchesAllowlist(key, false) {
			t.Errorf("matchesAllowlist(%q, posix) = true, want false", key)
		}
	}
	if matchesAllowlist("Proton_Key_Password", true) {
		t.Error("case folding must not admit variables outside the allowlist")
	}
}

func TestRedactBeforeTruncateKeepsSecretsOut(t *testing.T) {
	line := strings.Repeat("x", 230) + " could not read Password for 'https://uid-0123456789abcdef@proton-drive-cli.invalid'"
	out := Truncate(RedactText(line), 256)
	if strings.Contains(out, "uid-0123") {
		t.Fatalf("a cut inside the userinfo leaked part of it: %q", out)
	}
}

func TestRedactTextMasksCaptchaFields(t *testing.T) {
	out := RedactText("captcha_challenge=abc human-verification-code=def")
	if strings.Contains(out, "abc") || strings.Contains(out, "def") {
		t.Fatalf("RedactText() leaked a verification value: %q", out)
	}
}
