//go:build integration

package integration

import (
	"encoding/json"
	"testing"
)

func TestLiveDriveScopeRequestFromDoctorArgsMapsDataCredential(t *testing.T) {
	got, err := liveDriveScopeRequestFromDoctorArgs("--key-password-provider pass-cli --data-credential-provider pass-cli --data-credential-host proton-data.test --require-data-password")
	if err != nil {
		t.Fatalf("liveDriveScopeRequestFromDoctorArgs returned error: %v", err)
	}

	want := map[string]string{
		"folder":                 "/",
		"dataCredentialProvider": "pass-cli",
		"dataCredentialHost":     "proton-data.test",
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("request[%s] = %q, want %q; request=%v", key, got[key], value, got)
		}
	}
}

func TestLiveDriveScopeRequestFromDoctorArgsRejectsUnsupportedOption(t *testing.T) {
	_, err := liveDriveScopeRequestFromDoctorArgs("--key-password-provider pass-cli --username someone@example.test")
	if err == nil {
		t.Fatal("expected unsupported option to fail")
	}
}

func TestParseBridgeDetailsHandlesStringAndObject(t *testing.T) {
	asString, err := json.Marshal(`{"errorCode":"INSUFFICIENT_SCOPE","protonCode":9101}`)
	if err != nil {
		t.Fatalf("marshal string details: %v", err)
	}
	if got := parseBridgeDetails(asString); got["errorCode"] != "INSUFFICIENT_SCOPE" || got["protonCode"] != "9101" {
		t.Fatalf("string details parsed as %v", got)
	}

	asObject := json.RawMessage(`{"errorCode":"INSUFFICIENT_SCOPE","protonCode":9101}`)
	if got := parseBridgeDetails(asObject); got["errorCode"] != "INSUFFICIENT_SCOPE" || got["protonCode"] != "9101" {
		t.Fatalf("object details parsed as %v", got)
	}
}
