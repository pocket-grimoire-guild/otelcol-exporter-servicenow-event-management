//go:build integration

// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package traceexception

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testClientID           = "trace-exception-test-client"
	testClientSecret       = "trace-exception-test-secret"
	testToken              = "trace-exception-test-token"
	jsonV2Path             = "/api/global/em/jsonv2"
	spanKindServer   int32 = 2
	statusCodeUnset  int32 = 0
	statusCodeOK     int32 = 1
	statusCodeError  int32 = 2
)

type eventPayload struct {
	Records []eventRecord `json:"records"`
}

type eventRecord struct {
	Source          string `json:"source"`
	EventClass      string `json:"event_class"`
	Node            string `json:"node"`
	Resource        string `json:"resource"`
	MetricName      string `json:"metric_name"`
	Type            string `json:"type"`
	MessageKey      string `json:"message_key"`
	Severity        string `json:"severity"`
	Description     string `json:"description"`
	AdditionalInfo  string `json:"additional_info"`
	ResolutionState string `json:"resolution_state"`
	TimeOfEvent     string `json:"time_of_event"`
}

type fakeEvents struct {
	mu      sync.Mutex
	records []eventRecord
	bodies  [][]byte
	issues  []string
}

type fakeOAuth struct {
	mu       sync.Mutex
	requests int
	issues   []string
}

func TestMaintainedTraceExceptionExample(t *testing.T) {
	binary := os.Getenv("TRACE_EXCEPTION_COLLECTOR_BINARY")
	if binary == "" {
		t.Fatal("TRACE_EXCEPTION_COLLECTOR_BINARY is required; run make test-trace-exception")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("Collector binary is unavailable: %v", err)
	}
	yqBinary, err := exec.LookPath("yq")
	if err != nil {
		t.Fatalf("yq is required to edit temporary listener endpoints: %v", err)
	}
	examplePath := filepath.Join(repositoryRoot(), "examples", "servicenow-event-management-trace-exception-oauth.yaml")
	if _, err := os.Stat(examplePath); err != nil {
		t.Fatalf("maintained trace exception example is missing: %v", err)
	}

	t.Run("selected records and complete payload", func(t *testing.T) {
		oauthServer, oauth := newOAuthFake(t)
		eventServer, events := newEventFake(t)
		configPath, addresses := testConfig(t, yqBinary, examplePath, false)
		collector, err := startCollector(binary, configPath, filepath.Join(t.TempDir(), "collector.log"), map[string]string{
			"SERVICENOW_INSTANCE_URL":  eventServer.URL,
			"SERVICENOW_TOKEN_URL":     oauthServer.URL,
			"SERVICENOW_CLIENT_ID":     testClientID,
			"SERVICENOW_CLIENT_SECRET": testClientSecret,
		})
		if err != nil {
			t.Fatalf("start Collector: %v", err)
		}
		t.Cleanup(collector.forceStop)
		if err := waitUntilReady(collector, "http://"+addresses.health+"/", 20*time.Second); err != nil {
			t.Fatal(err)
		}
		status, body, err := sendTraces("http://"+addresses.otlp+"/v1/traces", traceFixture())
		if err != nil {
			t.Fatalf("send traces: %v", err)
		}
		if status != http.StatusOK {
			t.Fatalf("OTLP/HTTP returned %d, want %d: %s", status, http.StatusOK, body)
		}
		if err := collector.stop(30 * time.Second); err != nil {
			t.Fatal(err)
		}
		if issues := events.snapshotIssues(); len(issues) != 0 {
			t.Fatalf("JSON v2 fake rejected Collector requests: %s", strings.Join(issues, "; "))
		}
		if requests, issues := oauth.snapshot(); requests == 0 {
			t.Fatal("Collector did not use the OAuth token endpoint")
		} else if len(issues) != 0 {
			t.Fatalf("OAuth fake rejected Collector requests: %s", strings.Join(issues, "; "))
		}
		assertExpectedRecords(t, events.snapshotRecords(), events.snapshotBodies())
	})

	t.Run("propagated transform failure before batch", func(t *testing.T) {
		oauthServer, _ := newOAuthFake(t)
		eventServer, events := newEventFake(t)
		configPath, addresses := testConfig(t, yqBinary, examplePath, true)
		collector, err := startCollector(binary, configPath, filepath.Join(t.TempDir(), "collector-error.log"), map[string]string{
			"SERVICENOW_INSTANCE_URL":  eventServer.URL,
			"SERVICENOW_TOKEN_URL":     oauthServer.URL,
			"SERVICENOW_CLIENT_ID":     testClientID,
			"SERVICENOW_CLIENT_SECRET": testClientSecret,
		})
		if err != nil {
			t.Fatalf("start Collector: %v", err)
		}
		t.Cleanup(collector.forceStop)
		if err := waitUntilReady(collector, "http://"+addresses.health+"/", 20*time.Second); err != nil {
			t.Fatal(err)
		}
		selected := map[string]any{"resourceSpans": []any{resourceInput("checkout-api", "node-a", "prod", "trace-exception-test", "1.0.0",
			selectedSpan("00000000000000000000000000000031", "0000000000000031", "2026-09-27T12:00:00Z", "SelectedFailure", "error path", nil, nil))}}
		status, body, err := sendTraces("http://"+addresses.otlp+"/v1/traces", selected)
		if err != nil {
			t.Fatalf("send selected traces: %v", err)
		}
		if status != http.StatusServiceUnavailable {
			t.Fatalf("OTLP/HTTP returned %d, want %d for propagated ParseJSON failure: %s", status, http.StatusServiceUnavailable, body)
		}
		if err := collector.stop(30 * time.Second); err != nil {
			t.Fatal(err)
		}
		if len(events.snapshotRecords()) != 0 || len(events.snapshotBodies()) != 0 {
			t.Fatalf("ParseJSON failure produced a JSON v2 request with records=%d requests=%d", len(events.snapshotRecords()), len(events.snapshotBodies()))
		}
	})
}

type listenerAddresses struct {
	otlp   string
	health string
}

func testConfig(t *testing.T, yqBinary, examplePath string, injectFailure bool) (string, listenerAddresses) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "collector.yaml")
	config, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read maintained example: %v", err)
	}
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("copy maintained example: %v", err)
	}
	addresses := listenerAddresses{otlp: freeTCPAddress(t), health: freeTCPAddress(t)}
	help, _ := exec.Command(yqBinary, "--help").CombinedOutput()
	expression := ".receivers.otlp.protocols.http.endpoint = env.TRACE_EXCEPTION_OTLP_ADDRESS | .extensions.health_check.endpoint = env.TRACE_EXCEPTION_HEALTH_ADDRESS"
	if !strings.Contains(string(help), "jq filter") {
		expression = ".receivers.otlp.protocols.http.endpoint = strenv(TRACE_EXCEPTION_OTLP_ADDRESS) | .extensions.health_check.endpoint = strenv(TRACE_EXCEPTION_HEALTH_ADDRESS)"
	}
	if err := runYQ(yqBinary, configPath, expression, map[string]string{
		"TRACE_EXCEPTION_OTLP_ADDRESS":   addresses.otlp,
		"TRACE_EXCEPTION_HEALTH_ADDRESS": addresses.health,
	}); err != nil {
		t.Fatalf("set temporary listener endpoints: %v", err)
	}
	if injectFailure {
		expression := ".processors[\"transform/servicenow_event\"].log_statements[0].statements += [\"set(log.attributes[\\\"test.parse_failure\\\"], ParseJSON(\\\"not-json\\\"))\"]"
		if err := runYQ(yqBinary, configPath, expression, nil); err != nil {
			t.Fatalf("inject failing ParseJSON statement: %v", err)
		}
	}
	return configPath, addresses
}

func runYQ(yqBinary, configPath, expression string, variables map[string]string) error {
	help, _ := exec.Command(yqBinary, "--help").CombinedOutput()
	args := []string{"--yaml-output", "--in-place", expression, configPath}
	if !strings.Contains(string(help), "jq filter") {
		args = []string{"-i", expression, configPath}
	}
	command := exec.Command(yqBinary, args...)
	command.Env = environmentWith(variables, nil)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func repositoryRoot() string {
	_, sourceFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func newOAuthFake(t *testing.T) (*httptest.Server, *fakeOAuth) {
	t.Helper()
	capture := &fakeOAuth{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		clientID, secret := r.Form.Get("client_id"), r.Form.Get("client_secret")
		if id, password, ok := r.BasicAuth(); ok {
			clientID, secret = id, password
		}
		capture.mu.Lock()
		capture.requests++
		if r.Method != http.MethodPost || r.URL.Path != "/" || r.Form.Get("grant_type") != "client_credentials" || clientID != testClientID || secret != testClientSecret {
			capture.issues = append(capture.issues, "unexpected OAuth client-credentials request")
		}
		capture.mu.Unlock()
		if r.Method != http.MethodPost || r.Form.Get("grant_type") != "client_credentials" || clientID != testClientID || secret != testClientSecret {
			http.Error(w, "{\"error\":\"invalid_client\"}", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"access_token\":\""+testToken+"\",\"token_type\":\"Bearer\",\"expires_in\":3600}")
	}))
	t.Cleanup(server.Close)
	return server, capture
}

func newEventFake(t *testing.T) (*httptest.Server, *fakeEvents) {
	t.Helper()
	capture := &fakeEvents{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var issues []string
		if r.Method != http.MethodPost {
			issues = append(issues, "unexpected HTTP method")
		}
		if r.URL.Path != jsonV2Path || r.URL.RawQuery != "" {
			issues = append(issues, "unexpected JSON v2 route")
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			issues = append(issues, "OAuth bearer authorization was missing")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			issues = append(issues, "could not read JSON v2 request")
		}
		var payload eventPayload
		if err == nil {
			if decodeErr := json.Unmarshal(body, &payload); decodeErr != nil {
				issues = append(issues, "JSON v2 request was malformed")
			}
		}
		capture.mu.Lock()
		capture.bodies = append(capture.bodies, append([]byte(nil), body...))
		capture.records = append(capture.records, payload.Records...)
		capture.issues = append(capture.issues, issues...)
		capture.mu.Unlock()
		if len(issues) != 0 {
			http.Error(w, "{\"error\":\"unexpected request\"}", http.StatusBadRequest)
			return
		}
		successes := make([]map[string]string, len(payload.Records))
		for index := range successes {
			successes[index] = map[string]string{"__status": "success", "sys_id": fmt.Sprintf("fake-%d", index)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"records": successes})
	}))
	t.Cleanup(server.Close)
	return server, capture
}

func (c *fakeEvents) snapshotRecords() []eventRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]eventRecord(nil), c.records...)
}

func (c *fakeEvents) snapshotBodies() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	bodies := make([][]byte, len(c.bodies))
	for index := range c.bodies {
		bodies[index] = append([]byte(nil), c.bodies[index]...)
	}
	return bodies
}

func (c *fakeEvents) snapshotIssues() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.issues...)
}

func (c *fakeOAuth) snapshot() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests, append([]string(nil), c.issues...)
}

type collectorProcess struct {
	command  *exec.Cmd
	done     chan struct{}
	waitErr  error
	finished bool
	logPath  string
}

func startCollector(binary, config, logPath string, credentials map[string]string) (*collectorProcess, error) {
	absoluteBinary, err := filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	command := exec.Command(absoluteBinary, "--config", config)
	command.Stdout, command.Stderr = logFile, logFile
	command.Env = environmentWith(credentials, []string{"SERVICENOW_"})
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	done := make(chan struct{})
	process := &collectorProcess{command: command, done: done, logPath: logPath}
	go func() {
		process.waitErr = command.Wait()
		_ = logFile.Close()
		close(done)
	}()
	return process, nil
}

func (p *collectorProcess) stop(timeout time.Duration) error {
	select {
	case <-p.done:
		p.finished = true
		return fmt.Errorf("Collector exited before graceful shutdown: %v; %s", p.waitErr, p.logSummary())
	default:
	}
	if err := p.command.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("signal Collector for graceful shutdown: %w", err)
	}
	select {
	case <-p.done:
		p.finished = true
		if p.waitErr != nil {
			return fmt.Errorf("Collector exited during graceful shutdown: %w; %s", p.waitErr, p.logSummary())
		}
		return nil
	case <-time.After(timeout):
		_ = p.command.Process.Kill()
		<-p.done
		p.finished = true
		return errors.New("Collector did not shut down before deadline; forced cleanup was required")
	}
}

func (p *collectorProcess) forceStop() {
	if p.finished {
		return
	}
	_ = p.command.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
	}
	p.finished = true
}

func (p *collectorProcess) logSummary() string {
	content, err := os.ReadFile(p.logPath)
	if err != nil || len(content) == 0 {
		return "Collector log was empty"
	}
	if len(content) > 4096 {
		content = content[len(content)-4096:]
	}
	summary := string(content)
	for _, secret := range []string{testClientID, testClientSecret, testToken, "Bearer " + testToken} {
		summary = strings.ReplaceAll(summary, secret, "[redacted]")
	}
	return "Collector log tail: " + strings.TrimSpace(summary)
}

func waitUntilReady(process *collectorProcess, healthURL string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		request, _ := http.NewRequest(http.MethodGet, healthURL, nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-process.done:
			process.finished = true
			return fmt.Errorf("Collector exited before readiness: %v; %s", process.waitErr, process.logSummary())
		case <-deadline.C:
			return fmt.Errorf("health endpoint did not return HTTP 200")
		case <-ticker.C:
		}
	}
}

func sendTraces(endpoint string, payload any) (int, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, string(responseBody), err
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve temporary TCP address: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func environmentWith(values map[string]string, excludedPrefixes []string) []string {
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		excluded := false
		for _, prefix := range excludedPrefixes {
			if strings.HasPrefix(name, prefix) {
				excluded = true
				break
			}
		}
		if !excluded {
			env = append(env, entry)
		}
	}
	for name, value := range values {
		env = append(env, name+"="+value)
	}
	return env
}

type traceAttribute struct {
	key   string
	value any
}

func traceFixture() any {
	firstSelected := selectedSpan("00000000000000000000000000000001", "0000000000000001", "2026-09-27T12:00:01Z", "SelectedFailure", "MESSAGE-SENTINEL-ONE",
		[]traceAttribute{
			{"host.name", "span-host-shadow"}, {"deployment.environment.name", "span-environment-shadow"},
			{"exception.type", "ForgedSpanType"}, {"exception.message", "SPAN-MESSAGE-SENTINEL"}, {"exception.stacktrace", "SPAN-STACKTRACE-SENTINEL"},
			{"servicenow.description", "FORGED-DESCRIPTION-SENTINEL"}, {"servicenow.message_key", "FORGED-KEY-SENTINEL"},
			{"servicenow.severity", "0"}, {"servicenow.resolution_state", "Closing"}, {"servicenow.node", "FORGED-NODE-SENTINEL"},
			{"servicenow.source", "FORGED-SOURCE-SENTINEL"}, {"trace_id", "FORGED-TRACE-ALIAS"}, {"span_id", "FORGED-SPAN-ALIAS"},
			{"instrumentation_scope.name", "FORGED-SCOPE-NAME"}, {"instrumentation_scope.version", "FORGED-SCOPE-VERSION"},
			{"arbitrary.sentinel", "ARBITRARY-SENTINEL"},
		},
		[]traceAttribute{
			{"exception.message", "MESSAGE-SENTINEL-ONE"}, {"exception.stacktrace", "STACKTRACE-SENTINEL"},
			{"host.name", "event-host-shadow"}, {"deployment.environment.name", "event-environment-shadow"},
			{"servicenow.description", "FORGED-EVENT-DESCRIPTION-SENTINEL"},
			{"servicenow.message_key", "FORGED-EVENT-KEY-SENTINEL"}, {"servicenow.severity", "0"},
			{"servicenow.resolution_state", "Closing"}, {"servicenow.node", "FORGED-EVENT-NODE-SENTINEL"},
		})
	firstMap := firstSelected.(map[string]any)
	firstMap["events"] = append(firstMap["events"].([]any), map[string]any{
		"name":         "exception",
		"timeUnixNano": timestampNanos("2026-09-27T12:00:00Z"),
		"attributes":   attributesJSON([]traceAttribute{{"exception.type", "OtherFailure"}, {"exception.message", "NONSELECTED-EVENT-SENTINEL"}}),
	})
	spans := []any{
		firstSelected,
		selectedSpan("00000000000000000000000000000002", "0000000000000002", "2026-09-27T12:00:02Z", "SelectedFailure", "MESSAGE-SENTINEL-TWO", nil, nil),
	}
	resourceSpans := []any{resourceInput("checkout-api", "node-a", "prod", "trace-exception-test", "1.0.0", spans...)}
	resourceSpans = append(resourceSpans,
		resourceInput("checkout-api", "node-a", "prod", "", "", selectedSpan("00000000000000000000000000000000", "0000000000000000", "2026-09-27T12:00:03Z", "SelectedFailure", "MESSAGE-SENTINEL-ZERO-CONTEXT", nil, nil)),
		resourceInput("checkout-api", "node-a", "prod", "trace-exception-test", "1.0.0", selectedSpan("00000000000000000000000000000000", "0000000000000014", "2026-09-27T12:00:04Z", "SelectedFailure", "MESSAGE-SENTINEL-ZERO-TRACE", nil, nil)),
		resourceInput("checkout-api", "node-a", "prod", "", "", selectedSpan("00000000000000000000000000000015", "0000000000000000", "2026-09-27T12:00:05Z", "SelectedFailure", "MESSAGE-SENTINEL-ZERO-SPAN", nil, nil)),
		resourceInput("inventory-api", "node-a", "prod", "trace-exception-test", "1.0.0", selectedSpan("00000000000000000000000000000011", "0000000000000011", "2026-09-27T12:00:06Z", "SelectedFailure", "MESSAGE-SENTINEL-SERVICE", nil, nil)),
		resourceInput("checkout-api", "node-b", "prod", "trace-exception-test", "1.0.0", selectedSpan("00000000000000000000000000000017", "0000000000000017", "2026-09-27T12:00:07Z", "SelectedFailure", "MESSAGE-SENTINEL-HOST", nil, nil)),
		resourceInput("checkout-api", "node-a", "staging", "trace-exception-test", "1.0.0", selectedSpan("00000000000000000000000000000018", "0000000000000018", "2026-09-27T12:00:08Z", "SelectedFailure", "MESSAGE-SENTINEL-ENV", nil, nil)),
		resourceInput("checkout-api", "node-a", "prod", "trace-exception-test", "1.0.0",
			spanWithStatus("00000000000000000000000000000021", "0000000000000021", "2026-09-27T12:00:07Z", statusCodeError, "other exception", "exception", "OtherFailure", []traceAttribute{{"exception.type", "SelectedFailure"}}, nil),
			spanWithStatus("00000000000000000000000000000022", "0000000000000022", "2026-09-27T12:00:08Z", statusCodeError, "wrong event name", "exception_event", "SelectedFailure", nil, nil),
			spanWithStatus("00000000000000000000000000000023", "0000000000000023", "2026-09-27T12:00:09Z", statusCodeOK, "OK span", "exception", "SelectedFailure", nil, nil),
			spanWithStatus("00000000000000000000000000000024", "0000000000000024", "2026-09-27T12:00:10Z", statusCodeUnset, "unset span", "exception", "SelectedFailure", nil, nil),
			spanWithoutEvents("00000000000000000000000000000025", "0000000000000025", "2026-09-27T12:00:11Z", statusCodeError),
			spanWithStatus("00000000000000000000000000000026", "0000000000000026", "2026-09-27T12:00:12Z", statusCodeError, "forged span type", "exception", "OtherFailure", []traceAttribute{{"exception.type", "SelectedFailure"}}, nil),
			spanWithStatus("00000000000000000000000000000027", "0000000000000027", "2026-09-27T12:00:12Z", statusCodeError, "non-string type", "exception", true, nil, nil),
		),
	)
	return map[string]any{"resourceSpans": append(resourceSpans, invalidIdentityResources()...)}
}

func resourceInput(service, host, environment, scopeName, scopeVersion string, spans ...any) any {
	return resourceInputValues(map[string]any{
		"service.name": service, "host.name": host, "deployment.environment.name": environment,
		"producer.resource.sentinel": "RESOURCE-ARBITRARY-SENTINEL", "trace_id": "FORGED-RESOURCE-TRACE-ALIAS",
		"span_id": "FORGED-RESOURCE-SPAN-ALIAS", "instrumentation_scope.name": "FORGED-RESOURCE-SCOPE-NAME",
		"instrumentation_scope.version": "FORGED-RESOURCE-SCOPE-VERSION",
		"servicenow.description":        "FORGED-RESOURCE-DESCRIPTION-SENTINEL",
		"servicenow.message_key":        "FORGED-RESOURCE-KEY-SENTINEL",
		"servicenow.severity":           "0", "servicenow.resolution_state": "Closing",
		"servicenow.node": "FORGED-RESOURCE-NODE-SENTINEL", "servicenow.source": "FORGED-RESOURCE-SOURCE-SENTINEL",
	}, scopeName, scopeVersion, spans...)
}

func resourceInputValues(attributes map[string]any, scopeName, scopeVersion string, spans ...any) any {
	resourceAttributes := make([]any, 0, len(attributes))
	for key, value := range attributes {
		resourceAttributes = append(resourceAttributes, anyAttribute(key, value))
	}
	return map[string]any{
		"resource":   map[string]any{"attributes": resourceAttributes},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": scopeName, "version": scopeVersion}, "spans": spans}},
	}
}

func invalidIdentityResources() []any {
	resources := make([]any, 0, 9)
	for _, missing := range []string{"service.name", "host.name", "deployment.environment.name"} {
		attributes := map[string]any{"service.name": "checkout-api", "host.name": "node-a", "deployment.environment.name": "prod"}
		delete(attributes, missing)
		resources = append(resources, resourceInputValues(attributes, "trace-exception-test", "1.0.0",
			selectedSpan("00000000000000000000000000000041", "0000000000000041", "2026-09-27T12:00:13Z", "SelectedFailure", "missing identity", nil, nil)))
		attributes[missing] = ""
		resources = append(resources, resourceInputValues(attributes, "trace-exception-test", "1.0.0",
			selectedSpan("00000000000000000000000000000042", "0000000000000042", "2026-09-27T12:00:14Z", "SelectedFailure", "empty identity", nil, nil)))
		attributes[missing] = true
		resources = append(resources, resourceInputValues(attributes, "trace-exception-test", "1.0.0",
			selectedSpan("00000000000000000000000000000043", "0000000000000043", "2026-09-27T12:00:15Z", "SelectedFailure", "non-string identity", nil, nil)))
	}
	return resources
}

func selectedSpan(traceID, spanID, timestamp string, exceptionType, message any, spanAttributes, eventAttributes []traceAttribute) any {
	return spanWithStatus(traceID, spanID, timestamp, statusCodeError, message, "exception", exceptionType, spanAttributes, eventAttributes)
}

func spanWithoutEvents(traceID, spanID, timestamp string, status int32) any {
	return spanBase(traceID, spanID, timestamp, status, nil, nil)
}

func spanWithStatus(traceID, spanID, timestamp string, status int32, message any, eventName string, exceptionType any, spanAttributes, eventAttributes []traceAttribute) any {
	eventAttrs := append([]traceAttribute{{"exception.type", exceptionType}}, eventAttributes...)
	if message != nil && message != "" {
		eventAttrs = append(eventAttrs, traceAttribute{"exception.message", message})
		eventAttrs = append(eventAttrs, traceAttribute{"exception.stacktrace", "STACKTRACE-SENTINEL"})
	}
	event := map[string]any{"name": eventName, "timeUnixNano": timestampNanos(timestamp), "attributes": attributesJSON(eventAttrs)}
	return spanBase(traceID, spanID, timestamp, status, spanAttributes, []any{event})
}

func spanBase(traceID, spanID, timestamp string, status int32, spanAttributes []traceAttribute, events []any) any {
	eventTime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		panic(err)
	}
	return map[string]any{
		"traceId": traceID, "spanId": spanID, "name": "GET /checkout", "kind": spanKindServer,
		"startTimeUnixNano": strconv.FormatInt(eventTime.Add(-2*time.Second).UnixNano(), 10),
		"endTimeUnixNano":   strconv.FormatInt(eventTime.Add(time.Second).UnixNano(), 10),
		"attributes":        attributesJSON(spanAttributes), "events": events, "status": map[string]any{"code": status},
	}
}

func attributesJSON(attributes []traceAttribute) []any {
	result := make([]any, 0, len(attributes))
	for _, attribute := range attributes {
		result = append(result, anyAttribute(attribute.key, attribute.value))
	}
	return result
}

func anyAttribute(key string, value any) any {
	encoded := map[string]any{}
	switch typed := value.(type) {
	case string:
		encoded["stringValue"] = typed
	case bool:
		encoded["boolValue"] = typed
	case int:
		encoded["intValue"] = strconv.Itoa(typed)
	case int64:
		encoded["intValue"] = strconv.FormatInt(typed, 10)
	default:
		encoded["stringValue"] = fmt.Sprint(value)
	}
	return map[string]any{"key": key, "value": encoded}
}

func timestampNanos(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return strconv.FormatInt(parsed.UnixNano(), 10)
}

func assertExpectedRecords(t *testing.T, records []eventRecord, bodies [][]byte) {
	t.Helper()
	if len(records) != 8 {
		t.Fatalf("received %d event records after graceful shutdown, want exactly 8", len(records))
	}
	if len(bodies) == 0 {
		t.Fatal("JSON v2 fake received no requests")
	}
	forbidden := []string{
		"MESSAGE-SENTINEL", "STACKTRACE-SENTINEL", "SPAN-MESSAGE-SENTINEL", "SPAN-STACKTRACE-SENTINEL",
		"ARBITRARY-SENTINEL", "RESOURCE-ARBITRARY-SENTINEL", "FORGED-DESCRIPTION-SENTINEL", "FORGED-KEY-SENTINEL",
		"FORGED-NODE-SENTINEL", "FORGED-SOURCE-SENTINEL", "FORGED-TRACE-ALIAS", "FORGED-SPAN-ALIAS",
		"FORGED-SCOPE-NAME", "FORGED-SCOPE-VERSION", "FORGED-SEVERITY", "FORGED-RESOLUTION",
		"FORGED-EVENT-DESCRIPTION-SENTINEL", "FORGED-EVENT-KEY-SENTINEL", "FORGED-EVENT-NODE-SENTINEL",
		"FORGED-RESOURCE-DESCRIPTION-SENTINEL", "FORGED-RESOURCE-KEY-SENTINEL", "FORGED-RESOURCE-NODE-SENTINEL",
		"FORGED-RESOURCE-SOURCE-SENTINEL", "FORGED-RESOURCE-SPAN-ALIAS", "FORGED-RESOURCE-SCOPE-NAME",
		"FORGED-RESOURCE-TRACE-ALIAS", "FORGED-RESOURCE-SCOPE-VERSION", "NONSELECTED-EVENT-SENTINEL",
	}
	for _, body := range bodies {
		for _, sentinel := range forbidden {
			if bytes.Contains(body, []byte(sentinel)) {
				t.Fatalf("complete outgoing JSON v2 request contains forbidden sentinel %q", sentinel)
			}
		}
	}
	type expectedContext struct {
		service, host, environment string
		traceID, spanID            string
		scopeName, scopeVersion    string
	}
	expected := map[string]expectedContext{
		"2026-09-27 12:00:01": {"checkout-api", "node-a", "prod", "00000000000000000000000000000001", "0000000000000001", "trace-exception-test", "1.0.0"},
		"2026-09-27 12:00:02": {"checkout-api", "node-a", "prod", "00000000000000000000000000000002", "0000000000000002", "trace-exception-test", "1.0.0"},
		"2026-09-27 12:00:03": {"checkout-api", "node-a", "prod", "", "", "", ""},
		"2026-09-27 12:00:04": {"checkout-api", "node-a", "prod", "", "0000000000000014", "trace-exception-test", "1.0.0"},
		"2026-09-27 12:00:05": {"checkout-api", "node-a", "prod", "00000000000000000000000000000015", "", "", ""},
		"2026-09-27 12:00:06": {"inventory-api", "node-a", "prod", "00000000000000000000000000000011", "0000000000000011", "trace-exception-test", "1.0.0"},
		"2026-09-27 12:00:07": {"checkout-api", "node-b", "prod", "00000000000000000000000000000017", "0000000000000017", "trace-exception-test", "1.0.0"},
		"2026-09-27 12:00:08": {"checkout-api", "node-a", "staging", "00000000000000000000000000000018", "0000000000000018", "trace-exception-test", "1.0.0"},
	}
	seenKeys := map[string]map[string]bool{}
	for _, record := range records {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(record.AdditionalInfo), &decoded); err != nil {
			t.Fatalf("additional_info must be a JSON string: %v", err)
		}
		info := make(map[string]string, len(decoded))
		for key, value := range decoded {
			stringValue, ok := value.(string)
			if !ok {
				t.Fatalf("additional_info field %q is not string-valued", key)
			}
			info[key] = stringValue
		}
		if record.Source != "opentelemetry-trace-exception" || record.EventClass != "otel-trace-exception" ||
			record.Type != "trace-exception" || record.MetricName != "selected.exception" ||
			record.Severity != "2" || record.ResolutionState != "New" ||
			record.Description != "Selected exception observed" {
			t.Fatalf("unsafe or unexpected event fields: %+v", record)
		}
		context, ok := expected[record.TimeOfEvent]
		if !ok {
			t.Fatalf("unexpected event time %q", record.TimeOfEvent)
		}
		delete(expected, record.TimeOfEvent)
		if record.Resource != context.service || record.Node != context.host || info["service.name"] != context.service || info["host.name"] != context.host {
			t.Fatalf("resource identity did not remain authoritative: record=%+v info=%v", record, info)
		}
		if info["deployment.environment.name"] != context.environment || info["exception.type"] != "SelectedFailure" {
			t.Fatalf("selected dimensions are missing: %v", info)
		}
		if info["alert.rule_id"] != "demo.selected_exception" || info["alert.originating_signal"] != "traces" || info["otel.signal"] != "logs" {
			t.Fatalf("signal identity metadata is wrong: %v", info)
		}
		expectedKey := sha256IdentityKey(info["service.name"], info["host.name"], info["deployment.environment.name"], info["exception.type"])
		if record.MessageKey != expectedKey || len(record.MessageKey) != 79 || !strings.HasPrefix(record.MessageKey, "otel-sha256-v1:") {
			t.Fatalf("message_key %q does not match the configured sha256_v1 tuple %q", record.MessageKey, expectedKey)
		}
		if _, ok := info["otel.servicenow.message_key.strategy"]; ok {
			t.Fatalf("configured identity used fallback: %v", info)
		}
		if _, ok := info["otel.servicenow.message_key.missing_attributes"]; ok {
			t.Fatalf("configured identity reports missing attributes: %v", info)
		}
		if record.TimeOfEvent == "" {
			t.Fatalf("selected occurrence lost event time: %+v", record)
		}
		assertOptionalInfo(t, info, "trace_id", context.traceID)
		assertOptionalInfo(t, info, "span_id", context.spanID)
		assertOptionalInfo(t, info, "instrumentation_scope.name", context.scopeName)
		assertOptionalInfo(t, info, "instrumentation_scope.version", context.scopeVersion)
		for _, key := range []string{
			"exception.message", "exception.stacktrace", "arbitrary.sentinel", "producer.resource.sentinel",
			"servicenow.description", "servicenow.message_key", "servicenow.severity", "servicenow.resolution_state",
			"servicenow.node", "servicenow.source", "trace_id_alias", "span_id_alias",
		} {
			if _, ok := info[key]; ok {
				t.Fatalf("unapproved additional_info field %q survived: %v", key, info)
			}
		}
		dimension := strings.Join([]string{record.Resource, record.Node, info["deployment.environment.name"]}, "|")
		if seenKeys[dimension] == nil {
			seenKeys[dimension] = map[string]bool{}
		}
		seenKeys[dimension][record.MessageKey] = true
	}
	if len(expected) != 0 {
		t.Fatalf("some expected selected occurrences were missing: %v", expected)
	}
	baseKey := sha256IdentityKey("checkout-api", "node-a", "prod", "SelectedFailure")
	if len(seenKeys["checkout-api|node-a|prod"]) != 1 || !seenKeys["checkout-api|node-a|prod"][baseKey] {
		t.Fatalf("changed IDs, times, and messages changed the key for a fixed identity tuple: %v", seenKeys)
	}
	if len(seenKeys) != 4 {
		t.Fatalf("independent service, host, and environment changes did not produce four identity tuples: %v", seenKeys)
	}
}

func assertOptionalInfo(t *testing.T, info map[string]string, key, expected string) {
	t.Helper()
	actual, exists := info[key]
	if expected == "" {
		if exists {
			t.Fatalf("empty native context %q was copied: %v", key, info)
		}
		return
	}
	if !exists || actual != expected {
		t.Fatalf("native context %q is %q, want %q (info=%v)", key, actual, expected, info)
	}
}

func sha256IdentityKey(service, host, environment, exceptionType string) string {
	digest := sha256.New()
	writeFrame := func(value string) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write([]byte(value))
	}
	writeFrame("servicenow_event_management.message_key.sha256_v1")
	writeFrame("attributes")
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], 5)
	_, _ = digest.Write(count[:])
	for _, pair := range [][2]string{
		{"alert.rule_id", "demo.selected_exception"},
		{"service.name", service},
		{"host.name", host},
		{"deployment.environment.name", environment},
		{"exception.type", exceptionType},
	} {
		writeFrame(pair[0])
		writeFrame(pair[1])
	}
	return "otel-sha256-v1:" + hex.EncodeToString(digest.Sum(nil))
}
