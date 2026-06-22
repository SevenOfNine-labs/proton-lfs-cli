package preflight

import (
	"path/filepath"
	"strings"
	"testing"
)

var testAuthStates = []string{
	"ready",
	"login_available",
	"needs_login",
	"needs_data_password",
	"needs_key_password",
	"session_expired",
	"session_invalid",
	"configuration_error",
}

func doctorJSON(state string, transferReady bool, liveCanaryReady bool, extra string) []byte {
	return doctorJSONWithOK(true, state, transferReady, liveCanaryReady, extra)
}

func doctorJSONWithOK(ok bool, state string, transferReady bool, liveCanaryReady bool, extra string) []byte {
	if extra != "" {
		extra = "," + extra
	}
	return []byte(`{
		"ok": ` + boolJSON(ok) + `,
		"canAttemptTransfer": ` + boolJSON(transferReady) + `,
		"canAttemptLiveCanary": ` + boolJSON(liveCanaryReady) + `,
		"authState": {
			"state": "` + state + `",
			"actions": ["fix the local blocker"]
			` + extra + `
		}
	}`)
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestValidateDoctorReadinessAllowsLiveCanaryLoginAvailable(t *testing.T) {
	readiness, err := ValidateDoctorReadiness(
		doctorJSON("login_available", false, true, ""),
		testAuthStates,
		DoctorReadinessRequirements{RequireLiveCanary: true},
	)
	if err != nil {
		t.Fatalf("ValidateDoctorReadiness returned error: %v", err)
	}
	if readiness.AuthState.State != "login_available" {
		t.Fatalf("state = %q, want login_available", readiness.AuthState.State)
	}
}

func TestValidateDoctorReadinessAllowsReadyTransfer(t *testing.T) {
	readiness, err := ValidateDoctorReadiness(
		doctorJSON("ready", true, true, `"authMode":"srp"`),
		testAuthStates,
		DoctorReadinessRequirements{
			RequireTransfer: true,
			RequireState:    "ready",
		},
	)
	if err != nil {
		t.Fatalf("ValidateDoctorReadiness returned error: %v", err)
	}
	if !readiness.CanAttemptTransfer {
		t.Fatal("CanAttemptTransfer = false, want true")
	}
}

func TestValidateDoctorReadinessRejectsBlockedTransfer(t *testing.T) {
	_, err := ValidateDoctorReadiness(
		doctorJSON("needs_data_password", false, false, ""),
		testAuthStates,
		DoctorReadinessRequirements{RequireTransfer: true},
	)
	if err == nil {
		t.Fatal("expected blocked transfer error")
	}
	for _, want := range []string{"authState=needs_data_password", "fix the local blocker"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestValidateDoctorReadinessRejectsFailedDoctor(t *testing.T) {
	_, err := ValidateDoctorReadiness(
		doctorJSONWithOK(false, "ready", true, true, `"errors":["doctor failed"]`),
		testAuthStates,
		DoctorReadinessRequirements{RequireLiveCanary: true},
	)
	if err == nil {
		t.Fatal("expected failed doctor error")
	}
	for _, want := range []string{"offline doctor did not pass", "authState=ready", "doctor failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestValidateDoctorReadinessRejectsMalformedJSON(t *testing.T) {
	_, err := ValidateDoctorReadiness(
		[]byte(`{"ok":true`),
		testAuthStates,
		DoctorReadinessRequirements{},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func TestValidateDoctorReadinessRejectsMissingReadinessField(t *testing.T) {
	_, err := ValidateDoctorReadiness(
		[]byte(`{"ok":true,"canAttemptTransfer":true,"authState":{"state":"ready"}}`),
		testAuthStates,
		DoctorReadinessRequirements{RequireLiveCanary: true},
	)
	if err == nil || !strings.Contains(err.Error(), "canAttemptLiveCanary") {
		t.Fatalf("expected missing canAttemptLiveCanary error, got %v", err)
	}
}

func TestValidateDoctorReadinessRejectsUnknownAuthState(t *testing.T) {
	_, err := ValidateDoctorReadiness(
		doctorJSON("mystery_state", false, false, ""),
		testAuthStates,
		DoctorReadinessRequirements{},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown authState.state") {
		t.Fatalf("expected unknown state error, got %v", err)
	}
}

func TestValidateDoctorReadinessRequiresBrowserForkReadyTransfer(t *testing.T) {
	_, err := ValidateDoctorReadiness(
		doctorJSON("ready", true, true, `"authMode":"srp"`),
		testAuthStates,
		DoctorReadinessRequirements{
			RequireTransfer: true,
			RequireState:    "ready",
			RequireAuthMode: "browser-fork",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "auth mode mismatch") {
		t.Fatalf("expected auth mode mismatch, got %v", err)
	}

	_, err = ValidateDoctorReadiness(
		doctorJSON("ready", true, true, `"authMode":"browser-fork"`),
		testAuthStates,
		DoctorReadinessRequirements{
			RequireTransfer: true,
			RequireState:    "ready",
			RequireAuthMode: "browser-fork",
		},
	)
	if err != nil {
		t.Fatalf("expected browser-fork ready transfer to pass, got %v", err)
	}
}

func TestLoadBridgeAuthStatesFromSubmoduleSchema(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "submodules", "proton-drive-cli", "schemas", "bridge", "v1", "auth-state-payload.schema.json")
	states, err := LoadBridgeAuthStates(schemaPath)
	if err != nil {
		t.Fatalf("LoadBridgeAuthStates returned error: %v", err)
	}
	for _, want := range []string{"ready", "login_available", "needs_key_password"} {
		found := false
		for _, got := range states {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("schema states missing %q: %v", want, states)
		}
	}
}
