// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSummarizeSuccessResponseClassifiesReadInterruptions(t *testing.T) {
	tests := []struct {
		name        string
		readErr     error
		wantRetry   bool
		causeSecret string
	}{
		{name: "unexpected EOF", readErr: io.ErrUnexpectedEOF, wantRetry: true},
		{name: "wrapped unexpected EOF", readErr: &wrappedResponseReadError{cause: io.ErrUnexpectedEOF, message: "private read cause"}, wantRetry: true, causeSecret: "private read cause"},
		{name: "canceled", readErr: context.Canceled, wantRetry: true},
		{name: "wrapped deadline", readErr: &wrappedResponseReadError{cause: context.DeadlineExceeded, message: "private timeout detail"}, wantRetry: true, causeSecret: "private timeout detail"},
		{name: "wrapped network error", readErr: &wrappedResponseReadError{cause: syntheticNetworkError{}, message: "private network detail"}, wantRetry: true, causeSecret: "private network detail"},
		{name: "wrapped network timeout", readErr: &wrappedResponseReadError{cause: syntheticNetworkError{timeout: true}, message: "private timeout detail"}, wantRetry: true, causeSecret: "private timeout detail"},
		{name: "unknown", readErr: errors.New("private unknown body read error"), wantRetry: false, causeSecret: "private unknown body read error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := responseWithReadError([]byte("PARTIAL_CUSTOMER_EVENT"), tt.readErr)
			summary, retryable := summarizeSuccessResponse(resp)
			require.Contains(t, summary, "failed to read response body")
			require.Equal(t, tt.wantRetry, retryable)
			require.NotContains(t, summary, "PARTIAL_CUSTOMER_EVENT")
			if tt.causeSecret != "" {
				require.NotContains(t, summary, tt.causeSecret)
			}
		})
	}
}

func TestSummarizeSuccessResponseRetryBoundaryAtVerificationLimit(t *testing.T) {
	for _, tt := range []struct {
		name       string
		bodyLength int
		wantRetry  bool
	}{
		{name: "zero bytes", bodyLength: 0, wantRetry: true},
		{name: "exact limit", bodyLength: maxSuccessResponseBytes, wantRetry: true},
		{name: "one byte beyond limit", bodyLength: maxSuccessResponseBytes + 1, wantRetry: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := responseWithReadError(bytes.Repeat([]byte("x"), tt.bodyLength), io.ErrUnexpectedEOF)
			summary, retryable := summarizeSuccessResponse(resp)
			require.Contains(t, summary, "failed to read response body")
			require.Equal(t, tt.wantRetry, retryable)
		})
	}
}

func TestSummarizeSuccessResponseOversizedWhitespaceRemainsAccepted(t *testing.T) {
	body := bytes.Repeat([]byte(" "), maxSuccessResponseBytes+1)
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(bytes.NewReader(body)),
	}

	summary, retryable := summarizeSuccessResponse(resp)
	require.Empty(t, summary)
	require.False(t, retryable)
}

func TestClientSanitizesWrappedResponseReadCauseAndPartialBody(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "http://example.test"
	client := startClient(t, cfg, nil)
	var closed atomic.Bool
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: trackingResponseReadCloser{reader: &oneErrorReader{
				data: []byte(`{"message":"PARTIAL_EVENT_SECRET"}`),
				err:  &wrappedResponseReadError{cause: io.ErrUnexpectedEOF, message: "Authorization: Bearer WRAPPED_CAUSE_SECRET"},
			}, closed: &closed},
			Request: req,
		}, nil
	})}

	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "mode=instance api=jsonv2 status=200")
	require.Contains(t, err.Error(), "failed to read response body")
	require.NotContains(t, err.Error(), "PARTIAL_EVENT_SECRET")
	require.NotContains(t, err.Error(), "WRAPPED_CAUSE_SECRET")
	require.NotContains(t, err.Error(), "Authorization")
	require.True(t, closed.Load(), "response body must be closed after a read error")
}

func TestClientKeepsUnknownResponseReadErrorPermanent(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "http://example.test"
	client := startClient(t, cfg, nil)
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := responseWithReadError([]byte("PRIVATE_PARTIAL_UNKNOWN_BODY"), errors.New("PRIVATE_UNKNOWN_READ_CAUSE"))
		resp.Request = req
		return resp, nil
	})}

	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "failed to read response body")
	require.NotContains(t, err.Error(), "PRIVATE_PARTIAL_UNKNOWN_BODY")
	require.NotContains(t, err.Error(), "PRIVATE_UNKNOWN_READ_CAUSE")
}

func TestFactoryRetriesTruncatedResponseAcrossSupportedRoutes(t *testing.T) {
	for _, route := range serviceNowRoutes {
		t.Run(route.mode+"/"+route.api, func(t *testing.T) {
			var attempts atomic.Int32
			type observedRequest struct {
				path    string
				query   string
				payload []byte
			}
			requests := make(chan observedRequest, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				requests <- observedRequest{path: r.URL.Path, query: r.URL.RawQuery, payload: payload}
				if attempts.Add(1) == 1 {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Content-Length", "128")
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"records":[`))
					require.NoError(t, err)
					return
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			cfg := retryTestConfig(server.URL)
			cfg.Mode = route.mode
			cfg.API = route.api
			logsExporter := startRetryFactoryExporter(t, cfg, nil)
			logs := sampleLogs()
			before := snapshotLogsForTest(t, logs)
			logs.MarkReadOnly()
			require.NoError(t, logsExporter.ConsumeLogs(t.Context(), logs))
			require.Equal(t, int32(2), attempts.Load())
			require.Equal(t, before, snapshotLogsForTest(t, logs))

			var first observedRequest
			for i := 0; i < 2; i++ {
				select {
				case got := <-requests:
					if i == 0 {
						first = got
					} else {
						require.Equal(t, first.payload, got.payload)
						require.Equal(t, first.path, got.path)
						require.Equal(t, first.query, got.query)
					}
					require.Equal(t, route.path, got.path)
					require.Equal(t, route.rawQuery, got.query)
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for request retry")
				}
			}
		})
	}
}

func TestFactoryRetriesTimedOutSuccessfulResponseRead(t *testing.T) {
	var attempts atomic.Int32
	requests := make(chan []byte, 2)
	firstHeadersFlushed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requests <- payload
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(firstHeadersFlushed)
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	cfg.ClientConfig.Timeout = 150 * time.Millisecond
	logsExporter := startRetryFactoryExporter(t, cfg, nil)
	logs := sampleLogs()
	before := snapshotLogsForTest(t, logs)
	logs.MarkReadOnly()
	result := make(chan error, 1)
	go func() { result <- logsExporter.ConsumeLogs(context.Background(), logs) }()

	select {
	case <-firstHeadersFlushed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for flushed 2xx headers")
	}
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timed-out body retry")
	}
	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, before, snapshotLogsForTest(t, logs))
	first, second := receivePayloadBodies(t, requests, 2)
	require.Equal(t, first, second)
}

func TestFactoryDoesNotRetryBodyReadInterruptionWhenRetriesDisabled(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Length", "128")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"records":[`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	cfg.BackOffConfig.Enabled = false
	logsExporter := startRetryFactoryExporter(t, cfg, nil)
	err := logsExporter.ConsumeLogs(context.Background(), sampleLogs())
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
	require.Equal(t, int32(1), attempts.Load())
}

func TestFactoryStopsBodyReadRetryWhenCallerCancels(t *testing.T) {
	var attempts atomic.Int32
	firstHeadersFlushed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(firstHeadersFlushed)
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	cfg.ClientConfig.Timeout = 5 * time.Second
	logsExporter := startRetryFactoryExporter(t, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- logsExporter.ConsumeLogs(ctx, sampleLogs()) }()
	select {
	case <-firstHeadersFlushed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for flushed 2xx headers")
	}
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not stop response read promptly")
	}
	require.Equal(t, int32(1), attempts.Load())
}

func TestFactoryStopsPersistentReadRetriesAtFiniteElapsedBudget(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Length", "128")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"records":[`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	cfg.BackOffConfig.InitialInterval = 5 * time.Millisecond
	cfg.BackOffConfig.MaxInterval = 5 * time.Millisecond
	cfg.BackOffConfig.MaxElapsedTime = 40 * time.Millisecond
	logsExporter := startRetryFactoryExporter(t, cfg, nil)
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := logsExporter.ConsumeLogs(ctx, sampleLogs())
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
	require.Less(t, time.Since(started), time.Second)
	require.NoError(t, ctx.Err(), "finite retry budget should expire while caller context is still live")
}

func TestFactoryQueueContinuesAfterSuccessfulBodyReadRetry(t *testing.T) {
	var attempts atomic.Int32
	received := make(chan eventPayload, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		received <- payload
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Length", "128")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"records":[`))
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	queue := exporterhelper.NewDefaultQueueConfig()
	queue.NumConsumers = 1
	queue.Batch = configoptional.None[exporterhelper.BatchConfig]()
	cfg.QueueSettings = configoptional.Some(queue)
	observedCore, observed := observer.New(zapcore.DebugLevel)
	settings := exportertest.NewNopSettings(Type)
	settings.Logger = zap.New(observedCore)
	logsExporter, err := NewFactory().CreateLogs(context.Background(), settings, cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	shutdown := false
	t.Cleanup(func() {
		if !shutdown {
			require.NoError(t, logsExporter.Shutdown(context.Background()))
		}
	})
	first := logsWithRecords(1, 0)
	second := logsWithRecords(1, 100)
	first.MarkReadOnly()
	second.MarkReadOnly()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), first))
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), second))

	requests := make([]eventPayload, 0, 3)
	for len(requests) < 3 {
		select {
		case payload := <-received:
			requests = append(requests, payload)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for queued request progress: got %d of 3", len(requests))
		}
	}
	firstMetric := requests[0].Records[0].MetricName
	require.Equal(t, "queue.batch.000", firstMetric)
	require.Equal(t, firstMetric, requests[1].Records[0].MetricName)
	require.Equal(t, "queue.batch.100", requests[2].Records[0].MetricName)
	require.NoError(t, logsExporter.Shutdown(context.Background()))
	shutdown = true
	require.Empty(t, observed.FilterLevelExact(zapcore.ErrorLevel).All(), "retry must not log a permanent queue drop")
}

func TestFactoryKeepsKnownBodyRejectionPermanentWithRetriesEnabled(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"records":[{"__status":"failure"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	logsExporter := startRetryFactoryExporter(t, cfg, nil)
	err := logsExporter.ConsumeLogs(context.Background(), sampleLogs())
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "failed_records=1")
	require.Equal(t, int32(1), attempts.Load())
}

func TestFactoryKeepsInvalidGzipHeaderAndChecksumPermanent(t *testing.T) {
	validGzip := gzipTestBody(t, []byte(`{"records":[]}`))
	invalidChecksum := append([]byte(nil), validGzip...)
	invalidChecksum[len(invalidChecksum)-1] ^= 0xff
	for _, tt := range []struct {
		name string
		body []byte
	}{
		{name: "invalid header", body: []byte("PRIVATE_INVALID_GZIP_HEADER")},
		{name: "invalid checksum", body: invalidChecksum},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, err := w.Write(tt.body)
				require.NoError(t, err)
			}))
			defer server.Close()

			cfg := retryTestConfig(server.URL)
			logsExporter := startRetryFactoryExporter(t, cfg, nil)
			err := logsExporter.ConsumeLogs(context.Background(), sampleLogs())
			require.Error(t, err)
			require.True(t, consumererror.IsPermanent(err))
			require.Equal(t, int32(1), attempts.Load())
			require.NotContains(t, err.Error(), "PRIVATE_INVALID_GZIP_HEADER")
		})
	}
}

func TestFactoryHelperLogsOmitInterruptedPartialResponse(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "128")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"message":"PRIVATE_PARTIAL_EVENT Bearer PRIVATE_PARTIAL_TOKEN"}`))
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := retryTestConfig(server.URL)
	observedCore, observed := observer.New(zapcore.DebugLevel)
	logsExporter := startRetryFactoryExporter(t, cfg, zap.New(observedCore))
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), sampleLogs()))
	require.Equal(t, int32(2), attempts.Load())
	entries := observed.All()
	require.NotEmpty(t, entries, "exporterhelper should log the retry")
	var logsText strings.Builder
	for _, entry := range entries {
		logText := entry.Message + " " + fmt.Sprint(entry.Context)
		logsText.WriteString(logText)
		require.NotContains(t, logText, "PRIVATE_PARTIAL_EVENT")
		require.NotContains(t, logText, "PRIVATE_PARTIAL_TOKEN")
	}
	require.Contains(t, logsText.String(), "mode=mid")
	require.Contains(t, logsText.String(), "api=jsonv2")
	require.Contains(t, logsText.String(), "status=200")
	require.Contains(t, logsText.String(), "failed to read response body")
}

func TestExporterHelperLogsOmitWrappedResponseReadCause(t *testing.T) {
	cfg := retryTestConfig("http://example.test")
	cfg.Mode = ModeInstance
	runtime, err := newRuntimeConfig(cfg)
	require.NoError(t, err)
	client := newServiceNowClientFromRuntime(runtime, exportertest.NewNopSettings(Type).TelemetrySettings)
	require.NoError(t, client.start(context.Background(), hostWithExtensions{}))
	defer func() { require.NoError(t, client.shutdown(context.Background())) }()

	var attempts atomic.Int32
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: responseReadCloser{reader: &oneErrorReader{
					data: []byte(`{"message":"PRIVATE_HELPER_PARTIAL"}`),
					err:  &wrappedResponseReadError{cause: io.ErrUnexpectedEOF, message: "Authorization Bearer PRIVATE_WRAPPED_CAUSE"},
				}},
				Request: req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}

	observedCore, observed := observer.New(zapcore.DebugLevel)
	settings := exportertest.NewNopSettings(Type)
	settings.Logger = zap.New(observedCore)
	logsExporter, err := exporterhelper.NewLogs(
		context.Background(),
		settings,
		cfg,
		func(ctx context.Context, _ plog.Logs) error {
			return client.sendPayload(ctx, eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
		},
		exporterhelper.WithRetry(runtime.retry),
		exporterhelper.WithQueue(runtime.queue),
	)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	defer func() { require.NoError(t, logsExporter.Shutdown(context.Background())) }()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), sampleLogs()))
	require.Equal(t, int32(2), attempts.Load())

	entries := observed.All()
	require.NotEmpty(t, entries, "exporterhelper should log the wrapped cause retry")
	var logsText strings.Builder
	for _, entry := range entries {
		logsText.WriteString(entry.Message + " " + fmt.Sprint(entry.Context))
	}
	logText := logsText.String()
	require.Contains(t, logText, "mode=instance")
	require.Contains(t, logText, "api=jsonv2")
	require.Contains(t, logText, "status=200")
	require.Contains(t, logText, "failed to read response body")
	require.NotContains(t, logText, "PRIVATE_HELPER_PARTIAL")
	require.NotContains(t, logText, "PRIVATE_WRAPPED_CAUSE")
	require.NotContains(t, logText, "Authorization")
}

func retryTestConfig(endpoint string) *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = endpoint
	cfg.Mode = ModeMID
	cfg.API = APIJSONV2
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = true
	cfg.BackOffConfig.InitialInterval = time.Millisecond
	cfg.BackOffConfig.MaxInterval = time.Millisecond
	cfg.BackOffConfig.MaxElapsedTime = time.Second
	cfg.BackOffConfig.RandomizationFactor = 0
	return cfg
}

func startRetryFactoryExporter(t *testing.T, cfg *Config, logger *zap.Logger) exporter.Logs {
	t.Helper()
	settings := exportertest.NewNopSettings(Type)
	if logger != nil {
		settings.Logger = logger
	}
	logsExporter, err := NewFactory().CreateLogs(context.Background(), settings, cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	t.Cleanup(func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	})
	return logsExporter
}

func receivePayloadBodies(t *testing.T, bodies <-chan []byte, count int) ([]byte, []byte) {
	t.Helper()
	received := make([][]byte, 0, count)
	for len(received) < count {
		select {
		case body := <-bodies:
			received = append(received, body)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %d request payloads, received %d", count, len(received))
		}
	}
	return received[0], received[1]
}

func responseWithReadError(data []byte, err error) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       responseReadCloser{reader: &oneErrorReader{data: data, err: err}},
	}
}

type oneErrorReader struct {
	data []byte
	err  error
}

func (r *oneErrorReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.err != nil {
			err := r.err
			r.err = nil
			return 0, err
		}
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		err := r.err
		r.err = nil
		return n, err
	}
	return n, nil
}

type responseReadCloser struct {
	reader io.Reader
}

func (r responseReadCloser) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (responseReadCloser) Close() error                 { return nil }

type trackingResponseReadCloser struct {
	reader io.Reader
	closed *atomic.Bool
}

func (r trackingResponseReadCloser) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r trackingResponseReadCloser) Close() error {
	r.closed.Store(true)
	return nil
}

type wrappedResponseReadError struct {
	cause   error
	message string
}

func (e *wrappedResponseReadError) Error() string { return e.message }
func (e *wrappedResponseReadError) Unwrap() error { return e.cause }

type syntheticNetworkError struct {
	timeout bool
}

func (syntheticNetworkError) Error() string   { return "synthetic network timeout" }
func (e syntheticNetworkError) Timeout() bool { return e.timeout }
func (syntheticNetworkError) Temporary() bool { return false }

var _ net.Error = syntheticNetworkError{}

func gzipTestBody(t *testing.T, body []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}
