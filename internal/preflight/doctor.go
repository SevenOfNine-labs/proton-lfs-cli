// Package preflight contains local, no-network readiness checks used before
// guarded live canary targets are allowed to touch a Proton account.
package preflight

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// DoctorAuthState is the subset of proton-drive-cli doctor authState JSON used
// by root preflight gates.
type DoctorAuthState struct {
	State    string   `json:"state"`
	AuthMode string   `json:"authMode,omitempty"`
	Actions  []string `json:"actions,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

// DoctorReadiness is the parsed readiness surface emitted by
// `proton-drive-cli doctor --json`.
type DoctorReadiness struct {
	OK                   bool
	CanAttemptTransfer   bool
	CanAttemptLiveCanary bool
	AuthState            DoctorAuthState
}

// DoctorReadinessRequirements describes which doctor readiness gates must be
// true for the caller's operation.
type DoctorReadinessRequirements struct {
	RequireLiveCanary bool
	RequireTransfer   bool
	RequireState      string
	RequireAuthMode   string
}

type rawDoctorReport struct {
	OK                   *bool            `json:"ok"`
	CanAttemptTransfer   *bool            `json:"canAttemptTransfer"`
	CanAttemptLiveCanary *bool            `json:"canAttemptLiveCanary"`
	AuthState            *DoctorAuthState `json:"authState"`
}

type authStatePayloadSchema struct {
	Properties struct {
		State struct {
			Enum []string `json:"enum"`
		} `json:"state"`
	} `json:"properties"`
}

// LoadBridgeAuthStates reads the canonical bridge auth-state enum from the
// proton-drive-cli schema.
func LoadBridgeAuthStates(schemaPath string) ([]string, error) {
	body, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read bridge auth-state schema: %w", err)
	}

	var schema authStatePayloadSchema
	if err := json.Unmarshal(body, &schema); err != nil {
		return nil, fmt.Errorf("parse bridge auth-state schema: %w", err)
	}
	if len(schema.Properties.State.Enum) == 0 {
		return nil, fmt.Errorf("bridge auth-state schema has no state enum")
	}
	return schema.Properties.State.Enum, nil
}

// ValidateDoctorReadiness parses doctor JSON and verifies it satisfies the
// requested transfer or live-canary readiness gates.
func ValidateDoctorReadiness(
	body []byte,
	allowedAuthStates []string,
	requirements DoctorReadinessRequirements,
) (DoctorReadiness, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return DoctorReadiness{}, fmt.Errorf("offline doctor returned empty JSON")
	}

	allowed := make(map[string]struct{}, len(allowedAuthStates))
	for _, state := range allowedAuthStates {
		allowed[state] = struct{}{}
	}
	if len(allowed) == 0 {
		return DoctorReadiness{}, fmt.Errorf("no bridge auth-state values were provided")
	}
	if requirements.RequireState != "" {
		if _, ok := allowed[requirements.RequireState]; !ok {
			return DoctorReadiness{}, fmt.Errorf("required auth state %q is not declared by the bridge schema", requirements.RequireState)
		}
	}

	var raw rawDoctorReport
	if err := json.Unmarshal(body, &raw); err != nil {
		return DoctorReadiness{}, fmt.Errorf("offline doctor returned invalid JSON: %w", err)
	}
	if raw.OK == nil {
		return DoctorReadiness{}, fmt.Errorf("offline doctor JSON missing ok")
	}
	if raw.CanAttemptTransfer == nil {
		return DoctorReadiness{}, fmt.Errorf("offline doctor JSON missing canAttemptTransfer")
	}
	if raw.CanAttemptLiveCanary == nil {
		return DoctorReadiness{}, fmt.Errorf("offline doctor JSON missing canAttemptLiveCanary")
	}
	if raw.AuthState == nil {
		return DoctorReadiness{}, fmt.Errorf("offline doctor JSON missing authState")
	}
	if raw.AuthState.State == "" {
		return DoctorReadiness{}, fmt.Errorf("offline doctor JSON missing authState.state")
	}
	if _, ok := allowed[raw.AuthState.State]; !ok {
		return DoctorReadiness{}, fmt.Errorf("offline doctor returned unknown authState.state %q", raw.AuthState.State)
	}

	readiness := DoctorReadiness{
		OK:                   *raw.OK,
		CanAttemptTransfer:   *raw.CanAttemptTransfer,
		CanAttemptLiveCanary: *raw.CanAttemptLiveCanary,
		AuthState:            *raw.AuthState,
	}

	if !readiness.OK {
		return readiness, fmt.Errorf("offline doctor did not pass (%s)", readiness.blockerSummary())
	}
	if requirements.RequireState != "" && readiness.AuthState.State != requirements.RequireState {
		return readiness, fmt.Errorf("offline doctor auth state mismatch: want %s, got %s (%s)",
			requirements.RequireState,
			readiness.AuthState.State,
			readiness.blockerSummary(),
		)
	}
	if requirements.RequireAuthMode != "" && readiness.AuthState.AuthMode != requirements.RequireAuthMode {
		return readiness, fmt.Errorf("offline doctor auth mode mismatch: want %s, got %s (%s)",
			requirements.RequireAuthMode,
			emptyAsUnknown(readiness.AuthState.AuthMode),
			readiness.blockerSummary(),
		)
	}
	if requirements.RequireTransfer && !readiness.CanAttemptTransfer {
		return readiness, fmt.Errorf("offline doctor blocked transfers (%s)", readiness.blockerSummary())
	}
	if requirements.RequireLiveCanary && !readiness.CanAttemptLiveCanary {
		return readiness, fmt.Errorf("offline doctor blocked live canary (%s)", readiness.blockerSummary())
	}

	return readiness, nil
}

func (readiness DoctorReadiness) blockerSummary() string {
	parts := []string{"authState=" + readiness.AuthState.State}
	if readiness.AuthState.AuthMode != "" {
		parts = append(parts, "authMode="+readiness.AuthState.AuthMode)
	}
	if len(readiness.AuthState.Actions) > 0 {
		parts = append(parts, "actions="+strings.Join(readiness.AuthState.Actions, " | "))
	}
	if len(readiness.AuthState.Errors) > 0 {
		parts = append(parts, "errors="+strings.Join(readiness.AuthState.Errors, " | "))
	}
	return strings.Join(parts, "; ")
}

func emptyAsUnknown(value string) string {
	if value == "" {
		return "<unset>"
	}
	return value
}
