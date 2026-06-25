package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"proton-lfs-cli/internal/config"
)

const scopeLiveAckValue = "I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT"

var runScopeBridge = runDriveScopeBridgeCommand

type scopeBridgeEnvelope struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
	Code    int             `json:"code,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
}

type scopeDiagnosticsReport struct {
	GeneratedAt string                     `json:"generatedAt"`
	OK          bool                       `json:"ok"`
	Live        bool                       `json:"live"`
	Summary     string                     `json:"summary"`
	Safety      []string                   `json:"safety"`
	Config      scopeDiagnosticsConfig     `json:"config"`
	Session     scopeDiagnosticsSession    `json:"session"`
	Commands    []string                   `json:"commandsAttempted"`
	AuthState   *scopeDiagnosticsBridgeRun `json:"authState,omitempty"`
	LiveProbe   *scopeDiagnosticsBridgeRun `json:"liveProbe,omitempty"`
	Errors      []string                   `json:"errors,omitempty"`
}

type scopeDiagnosticsConfig struct {
	DriveCLIFound                  bool   `json:"driveCliFound"`
	DriveCLIPath                   string `json:"driveCliPath,omitempty"`
	KeyPasswordProvider            string `json:"keyPasswordProvider"`
	DataCredentialProvider         string `json:"dataCredentialProvider,omitempty"`
	DataCredentialHost             string `json:"dataCredentialHost,omitempty"`
	DataCredentialSource           string `json:"dataCredentialSource,omitempty"`
	StorageBase                    string `json:"storageBase"`
	AdapterRequestAppVersion       string `json:"adapterRequestAppVersion"`
	AdapterRequestAppVersionSource string `json:"adapterRequestAppVersionSource"`
	DriveCLIEnvAppVersion          string `json:"driveCliEnvAppVersion"`
	ExpectedDriveCLIDefault        string `json:"expectedDriveCliDefaultAppVersion"`
	DriveAPIBase                   string `json:"driveApiBase"`
}

type scopeDiagnosticsSession struct {
	Present                bool     `json:"present"`
	Path                   string   `json:"path,omitempty"`
	FileMode               string   `json:"fileMode,omitempty"`
	UIDFingerprint         string   `json:"uidFingerprint,omitempty"`
	SessionIDFingerprint   string   `json:"sessionIdFingerprint,omitempty"`
	Scopes                 []string `json:"scopes,omitempty"`
	PasswordMode           int      `json:"passwordMode,omitempty"`
	AuthMode               string   `json:"authMode,omitempty"`
	KeyPasswordPersisted   bool     `json:"keyPasswordPersisted,omitempty"`
	TokenExpiresAt         string   `json:"tokenExpiresAt,omitempty"`
	AccessTokenPresent     bool     `json:"accessTokenPresent,omitempty"`
	RefreshTokenPresent    bool     `json:"refreshTokenPresent,omitempty"`
	RawTokenValuesIncluded bool     `json:"rawTokenValuesIncluded"`
}

type scopeDiagnosticsBridgeRun struct {
	Attempted      bool           `json:"attempted"`
	Command        string         `json:"command"`
	NetworkRequest bool           `json:"networkRequest"`
	Target         string         `json:"target"`
	Request        map[string]any `json:"request,omitempty"`
	OK             bool           `json:"ok"`
	Code           int            `json:"code,omitempty"`
	Error          string         `json:"error,omitempty"`
	ErrorCode      string         `json:"errorCode,omitempty"`
	ProtonCode     int            `json:"protonCode,omitempty"`
	Details        map[string]any `json:"details,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	PayloadSummary map[string]any `json:"payloadSummary,omitempty"`
	HardStop       bool           `json:"hardStop,omitempty"`
	ProcessError   string         `json:"processError,omitempty"`
}

func cliScopeDiagnostics(w io.Writer, args []string) int {
	live, err := parseScopeDiagnosticsArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}

	prefs := config.LoadPrefs()
	dataProvider, dataHost, dataSource := resolveScopeDataCredential(prefs)
	driveCLI := findDriveCLI()

	report := scopeDiagnosticsReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		OK:          true,
		Live:        live,
		Summary:     "local diagnostics completed",
		Safety: []string{
			"local auth-state inspection must be offline-only",
			"live mode performs exactly one read-only bridge list request",
			"diagnostics never run login, init, upload, download, delete, or refresh",
			"raw token, password, and session values are not emitted",
		},
		Config: scopeDiagnosticsConfig{
			DriveCLIFound:                  driveCLI != "",
			DriveCLIPath:                   displayPath(driveCLI),
			KeyPasswordProvider:            prefs.CredentialProvider,
			DataCredentialProvider:         dataProvider,
			DataCredentialHost:             dataHost,
			DataCredentialSource:           dataSource,
			StorageBase:                    config.EnvOrDefault(config.EnvStorageBase, config.DefaultStorageBase),
			AdapterRequestAppVersion:       envOrUnset(config.EnvAppVersion),
			AdapterRequestAppVersionSource: appVersionSource(),
			DriveCLIEnvAppVersion:          envOrUnset("PROTON_DRIVE_CLI_APP_VERSION"),
			ExpectedDriveCLIDefault:        config.DefaultDriveCLIAppVersion,
			DriveAPIBase:                   "https://drive-api.proton.me",
		},
		Session: readScopeSessionDiagnostics(),
	}

	if driveCLI == "" {
		report.OK = false
		report.Summary = "proton-drive-cli not found"
		report.Errors = append(report.Errors, "proton-drive-cli not found")
		writeScopeDiagnostics(w, report)
		return 1
	}

	baseRequest := buildScopeBridgeRequest(dataProvider, dataHost)
	authRun := executeScopeBridgeRun(driveCLI, "auth-state", false, "local bridge auth-state inspection", baseRequest)
	report.AuthState = &authRun
	report.Commands = append(report.Commands, "bridge auth-state")
	if !authRun.OK {
		report.OK = false
		report.Summary = "auth-state diagnostics failed"
		report.Errors = append(report.Errors, authRun.Error)
		writeScopeDiagnostics(w, report)
		return 1
	}

	if !live {
		writeScopeDiagnostics(w, report)
		return 0
	}

	if state := bridgePayloadState(authRun.Payload); state != "ready" {
		report.OK = false
		report.Summary = "live scope probe skipped because auth-state is not ready"
		report.Errors = append(report.Errors, fmt.Sprintf("authState.state=%s", state))
		writeScopeDiagnostics(w, report)
		return 2
	}

	if strings.TrimSpace(os.Getenv("PROTON_LFS_LIVE_CANARY")) != scopeLiveAckValue {
		report.OK = false
		report.Summary = "live scope probe refused without acknowledgement"
		report.Errors = append(report.Errors, "set PROTON_LFS_LIVE_CANARY to the exact live canary acknowledgement to run the read-only server probe")
		writeScopeDiagnostics(w, report)
		return 2
	}

	liveRequest := cloneMap(baseRequest)
	liveRequest["folder"] = "/"
	liveRun := executeScopeBridgeRun(driveCLI, "list", true, "bridge list folder=/ via Proton Drive SDK", liveRequest)
	report.LiveProbe = &liveRun
	report.Commands = append(report.Commands, "bridge list")
	if !liveRun.OK {
		report.OK = false
		if liveRun.HardStop {
			report.Summary = "live scope probe hard-stopped at insufficient_scope"
			report.Errors = append(report.Errors, "Proton rejected the app/session authorization scope for Drive API calls")
			writeScopeDiagnostics(w, report)
			return 3
		}
		report.Summary = "live scope probe failed"
		report.Errors = append(report.Errors, liveRun.Error)
		writeScopeDiagnostics(w, report)
		return 1
	}

	report.Summary = "live scope probe passed"
	writeScopeDiagnostics(w, report)
	return 0
}

func parseScopeDiagnosticsArgs(args []string) (bool, error) {
	live := false
	for _, arg := range args {
		switch arg {
		case "--live":
			live = true
		case "":
			continue
		default:
			return false, fmt.Errorf("unknown scope-diagnostics option: %s", arg)
		}
	}
	return live, nil
}

func writeScopeDiagnostics(w io.Writer, report scopeDiagnosticsReport) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

func resolveScopeDataCredential(prefs config.Preferences) (provider, host, source string) {
	if provider = config.EnvTrim(config.EnvDataCredentialProvider); provider != "" {
		host = config.EnvOrDefault(config.EnvDataCredentialHost, config.DefaultDataCredentialHost)
		return provider, host, "env"
	}
	if provider = strings.TrimSpace(prefs.DataCredentialProvider); provider != "" {
		host = strings.TrimSpace(prefs.DataCredentialHost)
		if host == "" {
			host = config.DefaultDataCredentialHost
		}
		return provider, host, "prefs"
	}
	gitArgs := configuredDataCredentialArgsFromGit()
	if provider = parseFlagValue(gitArgs, "--data-credential-provider"); provider != "" {
		host = parseFlagValue(gitArgs, "--data-credential-host")
		if host == "" {
			host = config.DefaultDataCredentialHost
		}
		return provider, host, "git-config"
	}
	return "", "", ""
}

func configuredDataCredentialArgsFromGit() string {
	out, err := exec.Command("git", "config", "--global", "lfs.customtransfer.proton.args").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func buildScopeBridgeRequest(dataProvider, dataHost string) map[string]any {
	req := map[string]any{
		"storageBase": config.EnvOrDefault(config.EnvStorageBase, config.DefaultStorageBase),
	}
	if appVersion := config.EnvTrim(config.EnvAppVersion); appVersion != "" {
		req["appVersion"] = appVersion
	}
	if dataProvider != "" {
		req["dataCredentialProvider"] = dataProvider
		if dataHost != "" {
			req["dataCredentialHost"] = dataHost
		}
	}
	return req
}

func executeScopeBridgeRun(driveCLI, command string, network bool, target string, request map[string]any) scopeDiagnosticsBridgeRun {
	run := scopeDiagnosticsBridgeRun{
		Attempted:      true,
		Command:        "bridge " + command,
		NetworkRequest: network,
		Target:         target,
		Request:        redactMap(cloneMap(request)),
	}
	resp, err := runScopeBridge(driveCLI, command, request)
	if err != nil {
		run.ProcessError = redactDiagnosticString(err.Error())
		run.Error = run.ProcessError
		return run
	}
	run.OK = resp.OK
	run.Code = resp.Code
	run.Error = redactDiagnosticString(resp.Error)
	run.Details = parseScopeDetails(resp.Details)
	run.ErrorCode = stringFromAny(run.Details["errorCode"])
	run.ProtonCode = intFromAny(run.Details["protonCode"])
	run.HardStop = isInsufficientScopeRun(run)
	if command == "auth-state" {
		run.Payload = parseScopePayload(resp.Payload)
	} else {
		run.PayloadSummary = summarizeScopePayload(command, resp.Payload)
	}
	return run
}

func runDriveScopeBridgeCommand(driveCLI, command string, request map[string]any) (scopeBridgeEnvelope, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return scopeBridgeEnvelope{}, fmt.Errorf("marshal bridge request: %w", err)
	}
	cmd := exec.Command(driveCLI, "bridge", command)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Env = os.Environ()
	if passCLI := discoverPassCLIBinary(); passCLI != "" {
		cmd.Env = append(cmd.Env, "PROTON_PASS_CLI_BIN="+passCLI)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	resp, parseErr := parseScopeBridgeOutput(stdout.Bytes())
	if parseErr != nil {
		if stderr.Len() > 0 {
			return scopeBridgeEnvelope{}, fmt.Errorf("%w: %s", parseErr, redactDiagnosticString(stderr.String()))
		}
		if runErr != nil {
			return scopeBridgeEnvelope{}, fmt.Errorf("%w: %v", parseErr, runErr)
		}
		return scopeBridgeEnvelope{}, parseErr
	}
	return resp, nil
}

func parseScopeBridgeOutput(stdout []byte) (scopeBridgeEnvelope, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return scopeBridgeEnvelope{}, fmt.Errorf("empty stdout from bridge subprocess")
	}
	lines := bytes.Split(trimmed, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var resp scopeBridgeEnvelope
		if err := json.Unmarshal(line, &resp); err == nil {
			return resp, nil
		}
	}
	return scopeBridgeEnvelope{}, fmt.Errorf("no JSON bridge response found")
}

func readScopeSessionDiagnostics() scopeDiagnosticsSession {
	path := sessionFilePath()
	out := scopeDiagnosticsSession{
		Present:                false,
		Path:                   displayPath(path),
		RawTokenValuesIncluded: false,
	}
	if path == "" {
		return out
	}
	info, err := os.Stat(path)
	if err != nil {
		return out
	}
	out.Present = true
	out.FileMode = info.Mode().Perm().String()

	meta, ok := readSessionMetadata()
	if !ok {
		return out
	}
	out.UIDFingerprint = fingerprint(meta.UID)
	out.SessionIDFingerprint = fingerprint(meta.SessionID)
	out.Scopes = append([]string(nil), meta.Scopes...)
	sort.Strings(out.Scopes)
	out.PasswordMode = meta.PasswordMode
	out.AuthMode = meta.AuthMode
	out.KeyPasswordPersisted = meta.KeyPasswordPersisted
	if expiry := tokenExpiry(meta); !expiry.IsZero() {
		out.TokenExpiresAt = expiry.UTC().Format(time.RFC3339)
	}
	out.AccessTokenPresent = meta.AccessToken != ""
	out.RefreshTokenPresent = meta.RefreshToken != ""
	return out
}

func parseScopeDetails(raw json.RawMessage) map[string]any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var detailsString string
	if err := json.Unmarshal(raw, &detailsString); err == nil {
		detailsString = strings.TrimSpace(detailsString)
		if detailsString == "" {
			return nil
		}
		var details map[string]any
		if err := json.Unmarshal([]byte(detailsString), &details); err == nil {
			return redactMap(details)
		}
		return map[string]any{"raw": redactDiagnosticString(detailsString)}
	}
	var details map[string]any
	if err := json.Unmarshal(raw, &details); err == nil {
		return redactMap(details)
	}
	return map[string]any{"raw": redactDiagnosticString(string(raw))}
}

func parseScopePayload(raw json.RawMessage) map[string]any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{"raw": redactDiagnosticString(string(raw))}
	}
	return redactMap(payload)
}

func summarizeScopePayload(command string, raw json.RawMessage) map[string]any {
	summary := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return summary
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		summary["parseError"] = "payload omitted"
		return summary
	}
	if command == "list" {
		if files, ok := payload["files"].([]any); ok {
			summary["fileCount"] = len(files)
		}
		summary["fileNamesIncluded"] = false
		return summary
	}
	summary["included"] = false
	return summary
}

func isInsufficientScopeRun(run scopeDiagnosticsBridgeRun) bool {
	lowerErr := strings.ToLower(run.Error)
	return run.ErrorCode == "INSUFFICIENT_SCOPE" ||
		run.ProtonCode == 9101 ||
		strings.Contains(lowerErr, "9101") ||
		strings.Contains(lowerErr, "sufficient scope")
}

func bridgePayloadState(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(payload["state"]))
}

func redactMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = redactValue(key, value)
	}
	return out
}

func redactValue(key string, value any) any {
	if isSensitiveDiagnosticKey(key) {
		if value == nil || fmt.Sprint(value) == "" {
			return value
		}
		return "[redacted]"
	}
	switch v := value.(type) {
	case string:
		return redactDiagnosticString(v)
	case map[string]any:
		return redactMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactValue(key, item)
		}
		return out
	default:
		return value
	}
}

func isSensitiveDiagnosticKey(key string) bool {
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "provider") ||
		strings.Contains(lowerKey, "host") ||
		strings.HasPrefix(lowerKey, "has") ||
		strings.HasSuffix(lowerKey, "persisted") ||
		strings.HasSuffix(lowerKey, "available") ||
		strings.HasSuffix(lowerKey, "mode") {
		return false
	}
	return strings.Contains(lowerKey, "token") ||
		strings.Contains(lowerKey, "password") ||
		strings.Contains(lowerKey, "secret") ||
		strings.Contains(lowerKey, "sessionid") ||
		lowerKey == "uid"
}

func redactDiagnosticString(raw string) string {
	s := strings.TrimSpace(raw)
	for _, marker := range []string{
		"Bearer ",
		"accessToken",
		"refreshToken",
		"AccessToken",
		"RefreshToken",
		"token=",
		"password=",
		"session=",
	} {
		if idx := strings.Index(s, marker); idx >= 0 {
			return s[:idx] + "[redacted]"
		}
	}
	return s
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		return n
	default:
		return 0
	}
}

func envOrUnset(key string) string {
	if value := config.EnvTrim(key); value != "" {
		return value
	}
	return "(unset)"
}

func appVersionSource() string {
	if config.EnvTrim(config.EnvAppVersion) != "" {
		return config.EnvAppVersion
	}
	return "drive-cli default or PROTON_DRIVE_CLI_APP_VERSION"
}

func displayPath(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}

func fingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}
