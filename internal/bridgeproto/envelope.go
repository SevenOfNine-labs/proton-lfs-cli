// Package bridgeproto is the proton-drive-cli bridge contract shared by the
// adapter and the tray (submodules/proton-drive-cli/schemas/bridge/v1).
package bridgeproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Response is the envelope `proton-drive-cli bridge <command>` writes to
// stdout (schemas/bridge/v1/response-envelope.schema.json).
type Response struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
	Code    int             `json:"code,omitempty"`
	Details string          `json:"details,omitempty"`
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// parseEnvelope validates and decodes the strict bridge response envelope.
func parseEnvelope(raw []byte) (*Response, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("bridge response must be a JSON object")
	}

	for key := range fields {
		switch key {
		case "ok", "payload", "error", "code", "details":
		default:
			return nil, fmt.Errorf("bridge response contains unknown field %q", key)
		}
	}

	rawOK, ok := fields["ok"]
	if !ok {
		return nil, fmt.Errorf("bridge response missing required ok field")
	}
	if isJSONNull(rawOK) {
		return nil, fmt.Errorf("bridge response ok field must be boolean")
	}

	var resp Response
	if err := json.Unmarshal(rawOK, &resp.OK); err != nil {
		return nil, fmt.Errorf("bridge response ok field must be boolean: %w", err)
	}

	if rawPayload, ok := fields["payload"]; ok && !isJSONNull(rawPayload) {
		resp.Payload = append(json.RawMessage(nil), rawPayload...)
	}
	if rawError, ok := fields["error"]; ok {
		if isJSONNull(rawError) {
			return nil, fmt.Errorf("bridge response error field must be string")
		}
		if err := json.Unmarshal(rawError, &resp.Error); err != nil {
			return nil, fmt.Errorf("bridge response error field must be string: %w", err)
		}
	}
	if rawCode, ok := fields["code"]; ok {
		if isJSONNull(rawCode) {
			return nil, fmt.Errorf("bridge response code field must be integer")
		}
		if err := json.Unmarshal(rawCode, &resp.Code); err != nil {
			return nil, fmt.Errorf("bridge response code field must be integer: %w", err)
		}
	}
	if rawDetails, ok := fields["details"]; ok {
		if isJSONNull(rawDetails) {
			return nil, fmt.Errorf("bridge response details field must be string")
		}
		if err := json.Unmarshal(rawDetails, &resp.Details); err != nil {
			return nil, fmt.Errorf("bridge response details field must be string: %w", err)
		}
	}

	if resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return nil, fmt.Errorf("successful bridge response must not include an error message")
		}
		if resp.Code != 0 {
			return nil, fmt.Errorf("successful bridge response must not include an error code")
		}
		if resp.Details != "" {
			return nil, fmt.Errorf("successful bridge response must not include error details")
		}
		return &resp, nil
	}

	if strings.TrimSpace(resp.Error) == "" {
		return nil, fmt.Errorf("failed bridge response missing error message")
	}
	if resp.Code <= 0 {
		return nil, fmt.Errorf("failed bridge response missing positive error code")
	}
	if len(resp.Payload) > 0 {
		return nil, fmt.Errorf("failed bridge response must not include payload")
	}

	return &resp, nil
}

// ParseOutput extracts the JSON envelope from stdout: the whole output, or
// else its last line that is a JSON object (lines before it are ignored).
func ParseOutput(stdout []byte) (*Response, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty stdout from bridge subprocess")
	}

	if resp, err := parseEnvelope(trimmed); err == nil {
		return resp, nil
	} else if json.Valid(trimmed) {
		return nil, fmt.Errorf("invalid bridge response envelope: %w", err)
	}

	lines := bytes.Split(trimmed, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		resp, err := parseEnvelope(line)
		if err == nil {
			return resp, nil
		}
		if json.Valid(line) {
			return nil, fmt.Errorf("invalid bridge response envelope: %w", err)
		}
	}

	return nil, fmt.Errorf("no valid JSON envelope found in bridge output")
}
