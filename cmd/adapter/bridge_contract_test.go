package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type contractSchema struct {
	OneOf      []contractSchema           `json:"oneOf"`
	Properties map[string]json.RawMessage `json:"properties"`
}

type bridgeErrorCodeContract struct {
	ErrorCodes map[string]struct {
		HTTPStatus int    `json:"httpStatus"`
		RootCode   string `json:"rootCode"`
	} `json:"errorCodes"`
}

type bridgeRequestFieldRulesContract struct {
	Commands map[string]bridgeRequestFieldRule `json:"commands"`
}

type bridgeRequestFieldRule struct {
	Required []string `json:"required"`
	Allowed  []string `json:"allowed"`
}

func readBridgeContract[T any](t *testing.T, name string) T {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	path := filepath.Join(wd, "..", "..", "submodules", "proton-drive-cli", "schemas", "bridge", "v1", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read bridge contract %s: %v", name, err)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("failed to parse bridge contract %s: %v", name, err)
	}
	return value
}

func sortedKeys(raw map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrings(values []string) []string {
	cp := append([]string(nil), values...)
	sort.Strings(cp)
	return cp
}

func jsonFieldNames(value any) []string {
	typ := reflect.TypeOf(value)
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return fields
}

func enumValues(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var property struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(raw, &property); err != nil {
		t.Fatalf("failed to parse enum property: %v", err)
	}
	return sortedStrings(property.Enum)
}

func assertStringSlicesEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mismatch\ngot:  %v\nwant: %v", label, got, want)
	}
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func TestBridgeContractRequestFieldsAreKnown(t *testing.T) {
	schema := readBridgeContract[contractSchema](t, "request.schema.json")
	expected := sortedStrings([]string{
		"dataPassword",
		"dataCredentialProvider",
		"dataCredentialHost",
		"appVersion",
		"oid",
		"path",
		"outputPath",
		"folder",
		"storageBase",
		"oids",
	})

	assertStringSlicesEqual(t, "bridge request fields", sortedKeys(schema.Properties), expected)
}

func TestBridgeContractRequestFieldRulesCoverRootRequests(t *testing.T) {
	contract := readBridgeContract[bridgeRequestFieldRulesContract](t, "request-field-rules.json")
	baseCredentialFields := []string{
		"dataCredentialProvider",
		"dataCredentialHost",
		"storageBase",
		"appVersion",
	}

	cases := []struct {
		command string
		fields  []string
	}{
		{command: "auth-state", fields: baseCredentialFields},
		{command: "init", fields: baseCredentialFields},
		{command: "upload", fields: append(append([]string(nil), baseCredentialFields...), "oid", "path")},
		{command: "download", fields: append(append([]string(nil), baseCredentialFields...), "oid", "outputPath")},
		{command: "exists", fields: append(append([]string(nil), baseCredentialFields...), "oid")},
		{command: "delete", fields: append(append([]string(nil), baseCredentialFields...), "oid")},
		{command: "batch-exists", fields: append(append([]string(nil), baseCredentialFields...), "oids")},
		{command: "batch-delete", fields: append(append([]string(nil), baseCredentialFields...), "oids")},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			rule, ok := contract.Commands[tc.command]
			if !ok {
				t.Fatalf("bridge command %q has no request field rule", tc.command)
			}
			fields := stringSet(tc.fields)
			allowed := stringSet(rule.Allowed)
			for _, field := range tc.fields {
				if _, ok := allowed[field]; !ok {
					t.Fatalf("root sends field %q to bridge %s, but contract only allows %v", field, tc.command, rule.Allowed)
				}
			}
			for _, field := range rule.Required {
				if _, ok := fields[field]; !ok {
					t.Fatalf("bridge %s requires field %q, but root request shape has %v", tc.command, field, tc.fields)
				}
			}
		})
	}
}

func TestBridgeContractResponseEnvelopeFieldsMatchGoStruct(t *testing.T) {
	schema := readBridgeContract[contractSchema](t, "response-envelope.schema.json")
	if len(schema.OneOf) != 2 {
		t.Fatalf("expected response schema to define success and failure envelopes, got %d variants", len(schema.OneOf))
	}

	want := jsonFieldNames(BridgeResponse{})
	for idx, variant := range schema.OneOf {
		assertStringSlicesEqual(t, "bridge response fields variant "+string(rune('0'+idx)), sortedKeys(variant.Properties), want)
	}
}

func TestBridgeContractAuthStatePayloadFieldsMatchGoStruct(t *testing.T) {
	schema := readBridgeContract[contractSchema](t, "auth-state-payload.schema.json")
	assertStringSlicesEqual(t, "bridge auth-state payload fields", sortedKeys(schema.Properties), jsonFieldNames(BridgeAuthStateResponse{}))
}

func TestBridgeContractAuthStatesAreClassifiedForTransfer(t *testing.T) {
	schema := readBridgeContract[contractSchema](t, "auth-state-payload.schema.json")
	states := enumValues(t, schema.Properties["state"])
	expected := map[string]struct {
		code      int
		errorCode ErrorCode
		ok        bool
	}{
		"ready":               {ok: true},
		"needs_login":         {code: 401, errorCode: ErrCodeAuthRequired},
		"needs_data_password": {code: 401, errorCode: ErrCodeDataPasswordRequired},
		"needs_key_password":  {code: 401, errorCode: ErrCodeKeyPasswordRequired},
		"session_expired":     {code: 401, errorCode: ErrCodeAuthRequired},
		"session_invalid":     {code: 401, errorCode: ErrCodeAuthRequired},
		"configuration_error": {code: 400, errorCode: ErrCodeInvalidRequest},
	}

	for _, state := range states {
		want, ok := expected[state]
		if !ok {
			t.Fatalf("bridge auth state %q has no root transfer classification", state)
		}
		err := mapAuthStateForTransfer(&BridgeAuthStateResponse{State: state})
		if want.ok {
			if err != nil {
				t.Fatalf("state %q should be accepted, got %v", state, err)
			}
			continue
		}

		var backendErr *BackendError
		if !errors.As(err, &backendErr) {
			t.Fatalf("state %q should return BackendError, got %T %v", state, err, err)
		}
		if backendErr.Code != want.code || backendErr.ErrorCode != want.errorCode {
			t.Fatalf("state %q classified as code=%d errorCode=%s, want code=%d errorCode=%s",
				state, backendErr.Code, backendErr.ErrorCode, want.code, want.errorCode)
		}
	}
}

func TestBridgeContractErrorCodesAreClassified(t *testing.T) {
	contract := readBridgeContract[bridgeErrorCodeContract](t, "error-code-map.json")
	expected := map[string]ErrorCode{
		"AUTH_FAILED":            ErrCodeAuthRequired,
		"SESSION_EXPIRED":        ErrCodeAuthRequired,
		"INVALID_CREDENTIALS":    ErrCodeAuthRequired,
		"TWO_FACTOR_REQUIRED":    ErrCodeTwoFactorRequired,
		"DATA_PASSWORD_REQUIRED": ErrCodeDataPasswordRequired,
		"KEY_PASSWORD_REQUIRED":  ErrCodeKeyPasswordRequired,
		"NETWORK_ERROR":          ErrCodeNetworkFailure,
		"TIMEOUT":                ErrCodeNetworkFailure,
		"CONNECTION_REFUSED":     ErrCodeNetworkFailure,
		"API_ERROR":              ErrCodeServerError,
		"RATE_LIMITED":           ErrCodeRateLimited,
		"QUOTA_EXCEEDED":         ErrCodeServerError,
		"NOT_FOUND":              ErrCodeNotFound,
		"FILE_NOT_FOUND":         ErrCodeNotFound,
		"FILE_TOO_LARGE":         ErrCodeInvalidRequest,
		"PERMISSION_DENIED":      ErrCodePermissionDenied,
		"DISK_FULL":              ErrCodeServerError,
		"UPLOAD_FAILED":          ErrCodeServerError,
		"DOWNLOAD_FAILED":        ErrCodeServerError,
		"ENCRYPTION_FAILED":      ErrCodeServerError,
		"DECRYPTION_FAILED":      ErrCodeServerError,
		"INVALID_FILE":           ErrCodeInvalidRequest,
		"PATH_NOT_FOUND":         ErrCodeNotFound,
		"INVALID_PATH":           ErrCodeInvalidRequest,
		"NOT_A_FOLDER":           ErrCodeInvalidRequest,
		"CAPTCHA_REQUIRED":       ErrCodeCaptchaRequired,
		"UNKNOWN_ERROR":          ErrCodeServerError,
		"VALIDATION_ERROR":       ErrCodeInvalidRequest,
		"OPERATION_CANCELLED":    ErrCodeInvalidRequest,
	}

	if len(contract.ErrorCodes) != len(expected) {
		t.Fatalf("bridge error-code map has %d codes, root expects %d", len(contract.ErrorCodes), len(expected))
	}

	for code, entry := range contract.ErrorCodes {
		wantRoot, ok := expected[code]
		if !ok {
			t.Fatalf("bridge error code %q has no root classification", code)
		}
		if entry.RootCode != string(wantRoot) {
			t.Fatalf("bridge error code %q declares rootCode %q, root expects %q", code, entry.RootCode, wantRoot)
		}

		details, err := json.Marshal(map[string]string{"errorCode": code})
		if err != nil {
			t.Fatalf("failed to marshal details for %s: %v", code, err)
		}
		mapped := mapStructuredBridgeError(&BridgeError{
			Command: "contract",
			Code:    entry.HTTPStatus,
			Message: "contract " + code,
			Details: string(details),
		}, "contract fallback", errors.New("source"))

		var backendErr *BackendError
		if !errors.As(mapped, &backendErr) {
			t.Fatalf("bridge error code %q should map to BackendError, got %T %v", code, mapped, mapped)
		}
		if backendErr.Code != entry.HTTPStatus || backendErr.ErrorCode != wantRoot {
			t.Fatalf("bridge error code %q mapped to code=%d errorCode=%s, want code=%d errorCode=%s",
				code, backendErr.Code, backendErr.ErrorCode, entry.HTTPStatus, wantRoot)
		}
	}
}
