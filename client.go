// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	maxErrorResponseBytes      = 4096
	maxErrorResponseDrainBytes = 64 * 1024
	maxSuccessResponseBytes    = 4 * 1024 * 1024
)

const (
	instanceJSONV2Path  = "/api/global/em/jsonv2"
	midJSONV2Path       = "/api/mid/em/jsonv2"
	businessRulesPath   = "/em_event.do"
	businessRulesQuery  = "JSONv2&sysparm_action=insertMultiple"
	businessRulesAction = "insertMultiple"
)

var safeErrorCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)

type serviceNowRoute struct {
	mode     string
	api      string
	path     string
	rawQuery string
}

var serviceNowRoutes = []serviceNowRoute{
	{mode: ModeInstance, api: APIJSONV2, path: instanceJSONV2Path},
	{mode: ModeInstance, api: APIBusinessRules, path: businessRulesPath, rawQuery: businessRulesQuery},
	{mode: ModeMID, api: APIJSONV2, path: midJSONV2Path},
	{mode: ModeMID, api: APIBusinessRules, path: midJSONV2Path},
}

type serviceNowClient struct {
	runtime   runtimeConfig
	client    *http.Client
	telemetry component.TelemetrySettings
}

func newServiceNowClient(cfg *Config, telemetry component.TelemetrySettings) (*serviceNowClient, error) {
	runtime, err := newRuntimeConfig(cfg)
	if err != nil {
		return nil, err
	}
	return newServiceNowClientFromRuntime(runtime, telemetry), nil
}

func newServiceNowClientFromRuntime(runtime runtimeConfig, telemetry component.TelemetrySettings) *serviceNowClient {
	return &serviceNowClient{
		runtime:   runtime,
		telemetry: telemetry,
	}
}

func (c *serviceNowClient) start(ctx context.Context, host component.Host) error {
	httpClient, err := c.runtime.clientConfig.ToClient(ctx, host.GetExtensions(), c.telemetry)
	if err != nil {
		return err
	}
	c.client = httpClient
	return nil
}

func (c *serviceNowClient) shutdown(context.Context) error {
	return nil
}

func (c *serviceNowClient) sendPayload(ctx context.Context, payload eventPayload) error {
	if c.client == nil {
		return fmt.Errorf("ServiceNow Event Management HTTP client is not started")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return consumererror.NewPermanent(fmt.Errorf("marshal ServiceNow Event Management JSON v2 payload: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.runtime.endpoint, bytes.NewReader(body))
	if err != nil {
		return consumererror.NewPermanent(fmt.Errorf("create ServiceNow Event Management request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send ServiceNow Event Management request mode=%s api=%s: %w", c.runtime.mode, c.runtime.api, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		if responseSummary, retryableReadError := summarizeSuccessResponse(resp); responseSummary != "" {
			err := fmt.Errorf(
				"ServiceNow Event Management request failed mode=%s api=%s status=%d: %s",
				c.runtime.mode,
				c.runtime.api,
				resp.StatusCode,
				responseSummary,
			)
			if retryableReadError {
				return err
			}
			return consumererror.NewPermanent(err)
		}
		return nil
	}

	responseSummary := summarizeErrorResponse(resp)
	err = fmt.Errorf(
		"ServiceNow Event Management request failed mode=%s api=%s status=%d: %s",
		c.runtime.mode,
		c.runtime.api,
		resp.StatusCode,
		responseSummary,
	)
	if isRetryableStatus(resp.StatusCode) {
		return serviceNowThrottleRetry(err, resp)
	}
	return consumererror.NewPermanent(err)
}

func serviceNowEndpoint(cfg *Config) (string, error) {
	route, ok := serviceNowRouteFor(cfg.Mode, cfg.eventAPI())
	if !ok {
		return "", fmt.Errorf("unsupported ServiceNow route mode=%q api=%q", cfg.Mode, cfg.eventAPI())
	}
	return serviceNowEndpointFor(cfg.ClientConfig.Endpoint, route)
}

func serviceNowEndpointFor(endpoint string, route serviceNowRoute) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}

	cleanPath := strings.TrimRight(parsed.Path, "/")
	if cleanPath == "" {
		if parsed.RawQuery != "" {
			return "", unsupportedEndpointQueryError(route)
		}
		parsed.Path = route.path
		parsed.RawQuery = route.rawQuery
		return parsed.String(), nil
	}

	if strings.HasSuffix(cleanPath, route.path) {
		if route.rawQuery == "" {
			if parsed.RawQuery != "" {
				return "", unsupportedEndpointQueryError(route)
			}
			return parsed.String(), nil
		}
		if parsed.RawQuery == "" {
			parsed.RawQuery = route.rawQuery
		} else if !isBusinessRulesQuery(parsed.RawQuery) {
			return "", fmt.Errorf("endpoint query for mode %q api %q must include JSONv2 and sysparm_action=%s", route.mode, route.api, businessRulesAction)
		}
		return parsed.String(), nil
	}
	if isKnownServiceNowEventPath(cleanPath) {
		return "", fmt.Errorf("endpoint path %q does not match mode %q api %q", cleanPath, route.mode, route.api)
	}
	if strings.HasSuffix(cleanPath, "/jsonv2") {
		return "", fmt.Errorf("endpoint path %q is not a supported ServiceNow Event Management JSON v2 path for mode %q api %q", cleanPath, route.mode, route.api)
	}
	if parsed.RawQuery != "" {
		return "", unsupportedEndpointQueryError(route)
	}
	parsed.Path = cleanPath + route.path
	parsed.RawQuery = route.rawQuery
	return parsed.String(), nil
}

func unsupportedEndpointQueryError(route serviceNowRoute) error {
	return fmt.Errorf("endpoint query parameters are not supported for mode %q api %q; configure ServiceNow auth, routing, and proxy behavior with Collector auth, headers, tls, or proxy_url settings", route.mode, route.api)
}

func serviceNowRouteFor(mode, api string) (serviceNowRoute, bool) {
	for _, route := range serviceNowRoutes {
		if route.mode == mode && route.api == api {
			return route, true
		}
	}
	return serviceNowRoute{}, false
}

func isKnownServiceNowEventPath(cleanPath string) bool {
	for _, route := range serviceNowRoutes {
		if strings.HasSuffix(cleanPath, route.path) {
			return true
		}
	}
	return false
}

func isBusinessRulesQuery(rawQuery string) bool {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}
	jsonV2, hasJSONV2 := query["JSONv2"]
	action, hasAction := query["sysparm_action"]
	return len(query) == 2 &&
		hasJSONV2 &&
		len(jsonV2) == 1 &&
		jsonV2[0] == "" &&
		hasAction &&
		len(action) == 1 &&
		action[0] == businessRulesAction
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusTooManyRequests ||
		statusCode >= http.StatusInternalServerError
}

func serviceNowThrottleRetry(err error, resp *http.Response) error {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return err
	}

	// Match the upstream OTLP HTTP exporter pattern: interpret Retry-After at
	// the HTTP boundary, then let exporterhelper own the actual retry timing.
	values := resp.Header.Values("Retry-After")
	if len(values) == 0 {
		return err
	}
	if delay, ok := retryAfterDelay(values[0]); ok {
		return exporterhelper.NewThrottleRetry(err, delay)
	}
	return err
}

func retryAfterDelay(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		// Clamp before multiplication so an accepted value cannot wrap time.Duration.
		if int64(seconds) > int64(math.MaxInt64)/int64(time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(seconds) * time.Second, true
	}
	if date, err := http.ParseTime(value); err == nil {
		return time.Until(date), true
	}
	return 0, false
}

func summarizeErrorResponse(resp *http.Response) string {
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
	_, _ = io.CopyN(io.Discard, resp.Body, maxErrorResponseDrainBytes)
	if readErr != nil {
		return "response body omitted for data safety; failed to read error response"
	}
	if len(strings.TrimSpace(string(responseBody))) == 0 {
		return "empty response body"
	}

	summary := "response body omitted for data safety"
	if safeCode := safeServiceNowErrorCode(resp.Header.Get("Content-Type"), responseBody); safeCode != "" {
		summary += fmt.Sprintf("; error_code=%s", safeCode)
	}
	return summary
}

func summarizeSuccessResponse(resp *http.Response) (string, bool) {
	responseBody, tooLarge, readErr := readBoundedResponseBody(resp.Body, maxSuccessResponseBytes)
	_, _ = io.CopyN(io.Discard, resp.Body, maxErrorResponseDrainBytes)
	if readErr != nil {
		return "2xx response body omitted for data safety; failed to read response body", !tooLarge && isRetryableSuccessResponseReadError(readErr)
	}

	responseBody = bytes.TrimSpace(responseBody)
	if len(responseBody) == 0 {
		return "", false
	}
	if tooLarge {
		return "2xx response body too large to verify; response body omitted for data safety", false
	}
	if !isJSONResponseBody(resp.Header.Get("Content-Type"), responseBody) {
		return "unexpected non-JSON 2xx response; response body omitted for data safety", false
	}

	summary, err := summarizeServiceNow2xxJSONResponse(responseBody)
	if err != nil {
		return "invalid JSON 2xx response; response body omitted for data safety", false
	}
	return summary, false
}

func isRetryableSuccessResponseReadError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func readBoundedResponseBody(body io.Reader, limit int64) ([]byte, bool, error) {
	limited := &io.LimitedReader{R: body, N: limit + 1}
	responseBody, err := io.ReadAll(limited)
	if int64(len(responseBody)) > limit {
		return responseBody[:limit], true, err
	}
	return responseBody, false, err
}

func isJSONResponseBody(contentType string, responseBody []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "json") {
		return true
	}
	return len(responseBody) > 0 && (responseBody[0] == '{' || responseBody[0] == '[')
}

type serviceNow2xxResponse struct {
	Status       string            `json:"status"`
	JSONStatus   string            `json:"_status"`
	RecordStatus string            `json:"__status"`
	Error        json.RawMessage   `json:"error"`
	JSONError    json.RawMessage   `json:"_error"`
	RecordError  json.RawMessage   `json:"__error"`
	Result       json.RawMessage   `json:"result"`
	Records      []json.RawMessage `json:"records"`
}

type serviceNow2xxRecord struct {
	Status     string          `json:"__status"`
	JSONStatus string          `json:"_status"`
	Error      json.RawMessage `json:"__error"`
	JSONError  json.RawMessage `json:"_error"`
}

func summarizeServiceNow2xxJSONResponse(responseBody []byte) (string, error) {
	var decoded serviceNow2xxResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return "", err
	}

	failedRecords := 0
	for _, rawRecord := range decoded.Records {
		var record serviceNow2xxRecord
		if err := json.Unmarshal(rawRecord, &record); err != nil {
			continue
		}
		if isFailureStatus(record.Status) || isFailureStatus(record.JSONStatus) || hasNonNullJSONValue(record.Error) || hasNonNullJSONValue(record.JSONError) {
			failedRecords++
		}
	}
	if failedRecords > 0 {
		summary := fmt.Sprintf(
			"2xx response body reported ServiceNow record failure; failed_records=%d; response body omitted for data safety",
			failedRecords,
		)
		return withSafeResponseErrorCode(summary, responseBody), nil
	}

	if isFailureStatus(decoded.Status) ||
		isFailureStatus(decoded.JSONStatus) ||
		isFailureStatus(decoded.RecordStatus) ||
		hasTopLevelServiceNowError(decoded) {
		summary := "2xx response body reported ServiceNow failure; top_level_failure=true; response body omitted for data safety"
		return withSafeResponseErrorCode(summary, responseBody), nil
	}

	return "", nil
}

func isFailureStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "failure")
}

func hasTopLevelServiceNowError(response serviceNow2xxResponse) bool {
	if hasNonNullJSONValue(response.JSONError) || hasNonNullJSONValue(response.RecordError) {
		return true
	}
	return hasNonNullJSONValue(response.Error) && len(response.Result) == 0 && len(response.Records) == 0
}

func hasNonNullJSONValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null"))
}

func withSafeResponseErrorCode(summary string, responseBody []byte) string {
	if safeCode := safeServiceNowErrorCode("application/json", responseBody); safeCode != "" {
		return summary + fmt.Sprintf("; error_code=%s", safeCode)
	}
	return summary
}

func safeServiceNowErrorCode(contentType string, responseBody []byte) string {
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return ""
	}

	var decoded map[string]any
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return ""
	}

	for _, path := range [][]string{
		{"error", "code"},
		{"error", "error_code"},
		{"result", "error_code"},
		{"error_code"},
		{"code"},
	} {
		if code := nestedString(decoded, path...); safeErrorCodePattern.MatchString(code) {
			return code
		}
	}
	return ""
}

func nestedString(value any, path ...string) string {
	current := value
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = object[segment]
		if !ok {
			return ""
		}
	}
	text, ok := current.(string)
	if !ok {
		return ""
	}
	return text
}
