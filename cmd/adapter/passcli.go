package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func (a *Adapter) resolveSDKCredentials() error {
	if strings.ToLower(strings.TrimSpace(a.backendKind)) != BackendSDK {
		return nil
	}

	// Resolve username via pass-cli reference.
	if strings.TrimSpace(a.protonPassUserRef) != "" {
		value, err := resolvePassCLISecret(a.protonPassCLIBin, a.protonPassUserRef)
		if err != nil {
			return fmt.Errorf("username reference %q: %w", a.protonPassUserRef, err)
		}
		a.protonUsername = []byte(value)
	}

	// Resolve password via pass-cli reference.
	if strings.TrimSpace(a.protonPassPassRef) != "" {
		value, err := resolvePassCLISecret(a.protonPassCLIBin, a.protonPassPassRef)
		if err != nil {
			return fmt.Errorf("password reference %q: %w", a.protonPassPassRef, err)
		}
		a.protonPassword = []byte(value)
	}

	// If only password is referenced, derive username from signed-in Pass user info.
	if strings.TrimSpace(string(a.protonUsername)) == "" && strings.TrimSpace(a.protonPassUserRef) == "" && strings.TrimSpace(a.protonPassPassRef) != "" {
		value, err := resolvePassCLIUserEmail(a.protonPassCLIBin)
		if err == nil {
			a.protonUsername = []byte(value)
		}
	}

	// Credentials must be resolved via pass-cli; direct env var fallback is not supported.
	if strings.TrimSpace(string(a.protonUsername)) == "" {
		return fmt.Errorf("could not resolve Proton username via pass-cli (set PROTON_PASS_USERNAME_REF or PROTON_PASS_REF_ROOT)")
	}
	if strings.TrimSpace(string(a.protonPassword)) == "" {
		return fmt.Errorf("could not resolve Proton password via pass-cli (set PROTON_PASS_PASSWORD_REF or PROTON_PASS_REF_ROOT)")
	}

	return nil
}

func resolvePassCLISecret(passCLIBin string, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", fmt.Errorf("secret reference is empty")
	}

	bin := strings.TrimSpace(passCLIBin)
	if bin == "" {
		bin = DefaultPassCLIBinary
	}

	jsonValue, jsonErr := runPassCLISecretRead(bin, true, reference)
	if jsonErr == nil {
		return jsonValue, nil
	}

	plainValue, plainErr := runPassCLISecretRead(bin, false, reference)
	if plainErr == nil {
		return plainValue, nil
	}

	return "", fmt.Errorf("unable to read secret via %s (%v; fallback failed: %v)", bin, jsonErr, plainErr)
}

func resolvePassCLIUserEmail(passCLIBin string) (string, error) {
	bin := strings.TrimSpace(passCLIBin)
	if bin == "" {
		bin = DefaultPassCLIBinary
	}

	cmd := exec.Command(bin, "user", "info", "--output", "json")
	out, err := cmd.CombinedOutput()
	raw := strings.TrimSpace(string(out))
	if err != nil {
		if raw == "" {
			raw = err.Error()
		}
		return "", fmt.Errorf("command failed: %s", raw)
	}

	email := parsePassCLIUserEmail(raw)
	if email == "" {
		return "", fmt.Errorf("email not found in user info output")
	}
	return email, nil
}

func runPassCLISecretRead(bin string, preferJSON bool, reference string) (string, error) {
	args := []string{"item", "view"}
	if preferJSON {
		args = append(args, "--output", "json")
	}
	args = append(args, reference)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	raw := strings.TrimSpace(string(out))
	if err != nil {
		if raw == "" {
			raw = err.Error()
		}
		return "", fmt.Errorf("command failed: %s", raw)
	}

	value := parsePassCLISecretValue(raw)
	if value == "" {
		return "", fmt.Errorf("empty secret value")
	}
	return value, nil
}

func parsePassCLISecretValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		if value := jsonStringValue(decoded); value != "" {
			return value
		}
	}

	lines := strings.Split(trimmed, "\n")
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			if key == "value" || key == "secret" {
				line = strings.TrimSpace(parts[1])
			}
		}
		nonEmpty = append(nonEmpty, line)
	}

	if len(nonEmpty) == 0 {
		return ""
	}
	if len(nonEmpty) == 1 {
		return strings.TrimSpace(nonEmpty[0])
	}
	return strings.TrimSpace(nonEmpty[len(nonEmpty)-1])
}

func jsonStringValue(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"value", "secret", "content", "data", "text"} {
			if raw, ok := typed[key]; ok {
				if value := jsonStringValue(raw); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func parsePassCLIUserEmail(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		switch typed := decoded.(type) {
		case map[string]any:
			for _, key := range []string{"email", "username", "user", "login"} {
				if rawValue, ok := typed[key]; ok {
					if value := jsonStringValue(rawValue); value != "" {
						return value
					}
				}
			}
		}
	}

	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		if key == "email" || key == "username" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}
