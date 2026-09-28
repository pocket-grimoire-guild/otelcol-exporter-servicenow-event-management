//go:build integration

// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package metricsevent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testRunID        = "metrics-event-regression"
	metricName       = "demo.checkout.error_rate"
	testToken        = "metrics-event-test-token"
	testClientID     = "metrics-event-test-client"
	testClientSecret = "metrics-event-test-secret"
	serviceNowPath   = "/api/global/em/jsonv2"
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
}

type eventCapture struct {
	mu       sync.Mutex
	records  []eventRecord
	issues   []string
	observed chan struct{}
}

type oauthObservation struct {
	method           string
	path             string
	grantType        string
	credentialsValid bool
}

type oauthCapture struct {
	mu           sync.Mutex
	observations []oauthObservation
}

func TestMaintainedMetricsEventExample(t *testing.T) {
	collectorBinary := os.Getenv("METRICS_EVENT_COLLECTOR_BINARY")
	if collectorBinary == "" {
		t.Fatal("METRICS_EVENT_COLLECTOR_BINARY is required; run make test-metrics-event")
	}
	if _, err := os.Stat(collectorBinary); err != nil {
		t.Fatalf("Collector binary is unavailable: %v", err)
	}
	yqBinary, err := exec.LookPath("yq")
	if err != nil {
		t.Fatalf("yq is required to edit the temporary example listeners: %v", err)
	}
	repoRoot := repositoryRoot()
	templatePath := os.Getenv("METRICS_EVENT_TEST_CONFIG_TEMPLATE")
	if templatePath == "" {
		templatePath = filepath.Join(repoRoot, "examples", "servicenow-event-management-metrics-event-oauth.yaml")
	} else {
		if os.Getenv("METRICS_EVENT_TEST_ALLOW_CONFIG_OVERRIDE") != "1" {
			t.Fatal("METRICS_EVENT_TEST_CONFIG_TEMPLATE is restricted to explicit negative controls")
		}
		if !filepath.IsAbs(templatePath) {
			templatePath = filepath.Join(repoRoot, templatePath)
		}
	}

	oauthServer, oauth := newOAuthFake(t)
	eventServer, events := newEventFake(t)
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "collector.yaml")
	otlpAddress := freeTCPAddress(t)
	healthAddress := freeTCPAddress(t)
	if err := copyFile(templatePath, configPath); err != nil {
		t.Fatalf("copy metrics example config: %v", err)
	}
	if err := setTemporaryListeners(yqBinary, configPath, otlpAddress, healthAddress); err != nil {
		t.Fatalf("set temporary Collector listeners: %v", err)
	}

	collector, err := startCollector(collectorBinary, configPath, filepath.Join(workDir, "collector.log"), map[string]string{
		"SERVICENOW_INSTANCE_URL":         eventServer.URL,
		"SERVICENOW_TOKEN_URL":            oauthServer.URL,
		"SERVICENOW_CLIENT_ID":            testClientID,
		"SERVICENOW_CLIENT_SECRET":        testClientSecret,
		"SERVICENOW_METRICS_EVENT_RUN_ID": testRunID,
	})
	if err != nil {
		t.Fatalf("start Collector: %v", err)
	}
	defer collector.forceStop()

	readyErr := waitUntilReady(collector, "http://"+healthAddress+"/", 20*time.Second)
	var sendErr error
	var progressErr error
	if readyErr == nil {
		sendErr = sendMetrics("http://" + otlpAddress + "/v1/metrics")
		if sendErr == nil {
			progressErr = events.waitForRecords(3, 15*time.Second)
		}
	}
	shutdownErr := collector.stop(30 * time.Second)
	if readyErr != nil {
		t.Fatalf("Collector did not become ready: %v", readyErr)
	}
	if sendErr != nil {
		t.Fatalf("OTLP metrics request failed: %v", sendErr)
	}
	if progressErr != nil {
		t.Fatalf("no expected export progress before deadline: %v", progressErr)
	}
	if shutdownErr != nil {
		t.Fatalf("Collector graceful shutdown failed: %v", shutdownErr)
	}

	if issues := events.snapshotIssues(); len(issues) > 0 {
		t.Fatalf("ServiceNow fake rejected Collector requests: %s", strings.Join(issues, "; "))
	}
	if observations := oauth.snapshot(); len(observations) == 0 {
		t.Fatal("Collector did not use the OAuth token endpoint")
	} else {
		for _, observation := range observations {
			if observation.method != http.MethodPost || observation.path != "/" || observation.grantType != "client_credentials" || !observation.credentialsValid {
				t.Fatalf("OAuth client-credentials request was invalid (method=%s path=%s grant_type=%s credentials_valid=%t)", observation.method, observation.path, observation.grantType, observation.credentialsValid)
			}
		}
	}
	assertExpectedEvents(t, events.snapshotRecords())
}

func repositoryRoot() string {
	_, sourceFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func newOAuthFake(t *testing.T) (*httptest.Server, *oauthCapture) {
	t.Helper()
	capture := &oauthCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		clientID, clientSecret := r.Form.Get("client_id"), r.Form.Get("client_secret")
		if basicID, basicSecret, ok := r.BasicAuth(); ok {
			clientID, clientSecret = basicID, basicSecret
		}
		observation := oauthObservation{
			method:           r.Method,
			path:             r.URL.Path,
			grantType:        r.Form.Get("grant_type"),
			credentialsValid: clientID == testClientID && clientSecret == testClientSecret,
		}
		capture.mu.Lock()
		capture.observations = append(capture.observations, observation)
		capture.mu.Unlock()
		if observation.method != http.MethodPost || observation.grantType != "client_credentials" || !observation.credentialsValid {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"`+testToken+`","token_type":"Bearer","expires_in":3600}`)
	}))
	t.Cleanup(server.Close)
	return server, capture
}

func newEventFake(t *testing.T) (*httptest.Server, *eventCapture) {
	t.Helper()
	capture := &eventCapture{observed: make(chan struct{}, 64)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var issues []string
		if r.Method != http.MethodPost {
			issues = append(issues, "unexpected HTTP method")
		}
		if r.URL.Path != serviceNowPath || r.URL.RawQuery != "" {
			issues = append(issues, "unexpected JSON v2 route")
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			issues = append(issues, "expected OAuth bearer token was missing")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			issues = append(issues, "could not read JSON v2 body")
		}
		var payload eventPayload
		if err == nil {
			if decodeErr := json.Unmarshal(body, &payload); decodeErr != nil {
				issues = append(issues, "JSON v2 body was not valid event JSON")
			}
		}
		capture.mu.Lock()
		capture.records = append(capture.records, payload.Records...)
		capture.issues = append(capture.issues, issues...)
		capture.mu.Unlock()
		select {
		case capture.observed <- struct{}{}:
		default:
		}
		if len(issues) > 0 {
			http.Error(w, `{"error":"unexpected request"}`, http.StatusBadRequest)
			return
		}
		successes := make([]map[string]string, len(payload.Records))
		for i := range successes {
			successes[i] = map[string]string{"__status": "success", "sys_id": fmt.Sprintf("fake-%d", i)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"records": successes})
	}))
	t.Cleanup(server.Close)
	return server, capture
}

func (c *eventCapture) waitForRecords(minimum int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if len(c.snapshotRecords()) >= minimum {
			return nil
		}
		select {
		case <-c.observed:
		case <-deadline.C:
			return fmt.Errorf("received fewer than %d event records", minimum)
		}
	}
}

func (c *eventCapture) snapshotRecords() []eventRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]eventRecord(nil), c.records...)
}

func (c *eventCapture) snapshotIssues() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.issues...)
}

func (c *oauthCapture) snapshot() []oauthObservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]oauthObservation(nil), c.observations...)
}

type collectorProcess struct {
	command  *exec.Cmd
	done     chan struct{}
	waitErr  error
	finished bool
}

func startCollector(binary, config, logPath string, servicenowEnv map[string]string) (*collectorProcess, error) {
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
	command.Env = environmentWith(servicenowEnv, []string{"SERVICENOW_"})
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	done := make(chan struct{})
	process := &collectorProcess{command: command, done: done}
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
		select {
		case <-p.done:
			p.finished = true
			return fmt.Errorf("signal Collector for graceful shutdown: %v (process exit: %v); %s", err, p.waitErr, p.logSummary())
		case <-time.After(timeout):
			return fmt.Errorf("signal Collector for graceful shutdown: %w", err)
		}
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
		return fmt.Errorf("Collector did not shut down before deadline; forced cleanup was required")
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
	logPath := p.command.Stdout.(*os.File).Name()
	content, err := os.ReadFile(logPath)
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

func sendMetrics(endpoint string) error {
	body, err := json.Marshal(metricsFixture())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OTLP/HTTP returned %s", response.Status)
	}
	var result struct {
		PartialSuccess *struct {
			RejectedDataPoints json.RawMessage `json:"rejectedDataPoints"`
			ErrorMessage       string          `json:"errorMessage"`
		} `json:"partialSuccess"`
	}
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &result); err != nil {
			return fmt.Errorf("decode OTLP response: %w", err)
		}
	}
	if result.PartialSuccess != nil {
		rejected := int64(0)
		if len(result.PartialSuccess.RejectedDataPoints) > 0 {
			if err := json.Unmarshal(result.PartialSuccess.RejectedDataPoints, &rejected); err != nil {
				var encoded string
				if stringErr := json.Unmarshal(result.PartialSuccess.RejectedDataPoints, &encoded); stringErr != nil {
					return fmt.Errorf("decode OTLP rejected datapoint count: %w", err)
				}
				rejected, err = strconv.ParseInt(encoded, 10, 64)
				if err != nil {
					return fmt.Errorf("decode OTLP rejected datapoint count: %w", err)
				}
			}
		}
		if rejected != 0 || result.PartialSuccess.ErrorMessage != "" {
			return fmt.Errorf("OTLP response reported partial rejection (%d rejected datapoints)", rejected)
		}
	}
	return nil
}

type otlpAttribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
	} `json:"value"`
}

type otlpDataPoint struct {
	Attributes   []otlpAttribute `json:"attributes"`
	TimeUnixNano string          `json:"timeUnixNano"`
	AsDouble     float64         `json:"asDouble"`
}

type otlpMetric struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	Gauge       struct {
		DataPoints []otlpDataPoint `json:"dataPoints"`
	} `json:"gauge"`
}

type otlpScopeMetrics struct {
	Scope struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"scope"`
	Metrics []otlpMetric `json:"metrics"`
}

type otlpResourceMetrics struct {
	Resource struct {
		Attributes []otlpAttribute `json:"attributes"`
	} `json:"resource"`
	ScopeMetrics []otlpScopeMetrics `json:"scopeMetrics"`
}

type otlpMetrics struct {
	ResourceMetrics []otlpResourceMetrics `json:"resourceMetrics"`
}

func metricsFixture() otlpMetrics {
	now := strconv.FormatUint(uint64(time.Now().UnixNano()), 10)
	first := resourceMetrics("checkout-api", "metrics-node-a")
	first.ScopeMetrics[0].Metrics = append(first.ScopeMetrics[0].Metrics, gaugeMetric(metricName,
		point("/checkout", 91.5, now),
		point("/boundary", 90, now),
		point("/below", 89.5, now),
		point("/checkout", 94.25, now),
	))
	second := resourceMetrics("payments-api", "metrics-node-b")
	second.ScopeMetrics[0].Metrics = append(second.ScopeMetrics[0].Metrics,
		gaugeMetric(metricName, point("/checkout", 93.25, now)),
		gaugeMetric("demo.unrelated.high", point("/unrelated", 1000, now)),
	)
	return otlpMetrics{ResourceMetrics: []otlpResourceMetrics{first, second}}
}

func resourceMetrics(service, host string) otlpResourceMetrics {
	var result otlpResourceMetrics
	result.Resource.Attributes = []otlpAttribute{stringAttribute("service.name", service), stringAttribute("host.name", host)}
	var scope otlpScopeMetrics
	scope.Scope.Name, scope.Scope.Version = "metrics-event-regression", "0.1.0"
	result.ScopeMetrics = []otlpScopeMetrics{scope}
	return result
}

func gaugeMetric(name string, points ...otlpDataPoint) otlpMetric {
	metric := otlpMetric{Name: name, Description: "Synthetic checkout error rate", Unit: "%"}
	metric.Gauge.DataPoints = points
	return metric
}

func point(route string, value float64, now string) otlpDataPoint {
	return otlpDataPoint{Attributes: []otlpAttribute{stringAttribute("route", route)}, TimeUnixNano: now, AsDouble: value}
}

func stringAttribute(key, value string) otlpAttribute {
	return otlpAttribute{Key: key, Value: struct {
		StringValue string `json:"stringValue"`
	}{StringValue: value}}
}

func assertExpectedEvents(t *testing.T, records []eventRecord) {
	t.Helper()
	expected := map[string]struct {
		service string
		host    string
		route   string
		value   string
	}{
		"metrics-node-a|91.5":  {"checkout-api", "metrics-node-a", "/checkout", "91.5"},
		"metrics-node-a|94.25": {"checkout-api", "metrics-node-a", "/checkout", "94.25"},
		"metrics-node-b|93.25": {"payments-api", "metrics-node-b", "/checkout", "93.25"},
	}
	if len(records) != len(expected) {
		t.Fatalf("received %d event records, want exactly %d", len(records), len(expected))
	}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		var info map[string]any
		if err := json.Unmarshal([]byte(record.AdditionalInfo), &info); err != nil {
			t.Fatalf("additional_info must be a JSON string containing string-valued data: %v", err)
		}
		stringInfo := make(map[string]string, len(info))
		for key, value := range info {
			stringValue, ok := value.(string)
			if !ok {
				t.Fatalf("additional_info value %q must be a JSON string", key)
			}
			stringInfo[key] = stringValue
		}
		value := stringInfo["gauge.value"]
		identity := record.Node + "|" + value
		want, ok := expected[identity]
		if !ok {
			t.Fatalf("unexpected event identity/value tuple %q", identity)
		}
		if seen[identity] {
			t.Fatalf("duplicate event identity/value tuple %q", identity)
		}
		seen[identity] = true
		wantKey := strings.Join([]string{testRunID, want.service, want.host, metricName, want.route}, "|")
		if record.Source != "opentelemetry-metrics-threshold" || record.EventClass != "otel-metric-threshold" || record.Type != "metric-threshold" || record.MetricName != metricName || record.Resource != want.service || record.Node != want.host || record.Severity != "3" || record.ResolutionState != "New" || record.MessageKey != wantKey {
			t.Fatalf("mapped event fields did not match the example contract for %q", identity)
		}
		if !strings.Contains(record.Description, "Metric threshold breach") || !strings.Contains(record.Description, metricName) || !strings.Contains(record.Description, want.value) {
			t.Fatalf("description did not include the breach metric and value for %q", identity)
		}
		if stringInfo["alert.threshold"] != "90" || stringInfo["gauge.value"] != want.value || stringInfo["route"] != want.route || stringInfo["test.run.id"] != testRunID {
			t.Fatalf("additional_info fields did not match the example contract for %q", identity)
		}
	}
	if !seen["metrics-node-a|91.5"] || !seen["metrics-node-a|94.25"] || !seen["metrics-node-b|93.25"] {
		keys := make([]string, 0, len(seen))
		for key := range seen {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		t.Fatalf("event multiset incomplete: %v", keys)
	}
}

func setTemporaryListeners(yqBinary, configPath, otlpAddress, healthAddress string) error {
	help, _ := exec.Command(yqBinary, "--help").CombinedOutput()
	var args []string
	if bytes.Contains(help, []byte("jq filter")) {
		args = []string{"--yaml-output", "--in-place", ".receivers.otlp.protocols.http.endpoint = env.METRICS_EVENT_OTLP_LISTENER | .extensions.health_check.endpoint = env.METRICS_EVENT_HEALTH_LISTENER", configPath}
	} else {
		args = []string{"-i", ".receivers.otlp.protocols.http.endpoint = strenv(METRICS_EVENT_OTLP_LISTENER) | .extensions.health_check.endpoint = strenv(METRICS_EVENT_HEALTH_LISTENER)", configPath}
	}
	command := exec.Command(yqBinary, args...)
	command.Env = environmentWith(map[string]string{
		"METRICS_EVENT_OTLP_LISTENER":   otlpAddress,
		"METRICS_EVENT_HEALTH_LISTENER": healthAddress,
	}, nil)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("yq failed to edit temporary listeners: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyFile(source, destination string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, content, 0o600)
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback listener: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release temporary loopback listener: %v", err)
	}
	return address
}

func environmentWith(overrides map[string]string, dropPrefixes []string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		drop := false
		for _, prefix := range dropPrefixes {
			if strings.HasPrefix(key, prefix) {
				drop = true
				break
			}
		}
		if !drop {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
