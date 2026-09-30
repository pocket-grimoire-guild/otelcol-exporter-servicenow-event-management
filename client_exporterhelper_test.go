// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogsExporterConsumesAndSendsPayload(t *testing.T) {
	received := make(chan eventPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = false
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

	logsExporter, err := createLogsExporter(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.NoError(t, err)
	require.False(t, logsExporter.Capabilities().MutatesData)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	defer func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	}()

	logs := sampleLogs()
	logs.MarkReadOnly()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))

	select {
	case payload := <-received:
		require.Len(t, payload.Records, 1)
		require.Equal(t, "checkout|host-01|request.error_rate", payload.Records[0].MessageKey)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fake ServiceNow endpoint")
	}
}

func TestFactoryLogsExporterSendsValidatedSeverityAndClearState(t *testing.T) {
	type requestRecord struct {
		method  string
		path    string
		records []map[string]json.RawMessage
	}

	received := make(chan requestRecord, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Records []map[string]json.RawMessage `json:"records"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		received <- requestRecord{method: r.Method, path: r.URL.Path, records: payload.Records}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	newFactoryExporter := func(cfg *Config) exporter.Logs {
		t.Helper()
		logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
		require.NoError(t, err)
		require.False(t, logsExporter.Capabilities().MutatesData)
		require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
		t.Cleanup(func() {
			require.NoError(t, logsExporter.Shutdown(context.Background()))
		})
		return logsExporter
	}
	assertRequest := func(wantSeverities []string, wantResolutionStates []string) {
		t.Helper()
		select {
		case request := <-received:
			require.Equal(t, http.MethodPost, request.method)
			require.Equal(t, "/api/mid/em/jsonv2", request.path)
			require.Len(t, request.records, len(wantSeverities))
			require.Len(t, wantResolutionStates, len(wantSeverities))
			for i, rawRecord := range request.records {
				var severity string
				require.NoError(t, json.Unmarshal(rawRecord["severity"], &severity))
				require.Equal(t, wantSeverities[i], severity)
				if wantResolutionStates[i] == "" {
					_, present := rawRecord["resolution_state"]
					require.False(t, present)
					continue
				}
				var resolutionState string
				require.NoError(t, json.Unmarshal(rawRecord["resolution_state"], &resolutionState))
				require.Equal(t, wantResolutionStates[i], resolutionState)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for fake ServiceNow endpoint")
		}
	}

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = false
	logsExporter := newFactoryExporter(cfg)
	logs := logsWithRecords(3, 0)
	logRecords := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	logRecords.At(0).Attributes().PutDouble("servicenow.severity", -0.5)
	logRecords.At(1).SetSeverityNumber(plog.SeverityNumberWarn)
	logRecords.At(1).Attributes().PutDouble("servicenow.severity", 0.9)
	logRecords.At(2).Attributes().PutInt("servicenow.severity", 0)
	logs.MarkReadOnly()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))
	assertRequest([]string{"2", "4", "0"}, []string{"", "", "Closing"})

	clearCfg := createDefaultConfig().(*Config)
	clearCfg.ClientConfig.Endpoint = server.URL
	clearCfg.Mode = ModeMID
	clearCfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	clearCfg.BackOffConfig.Enabled = false
	clearCfg.Severity.Mapping.Error = 0
	clearCfg.Severity.ClearResolutionState = ""
	clearExporter := newFactoryExporter(clearCfg)
	clearLogs := logsWithRecords(1, 3)
	clearLogs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutDouble("servicenow.severity", 5.9)
	clearLogs.MarkReadOnly()
	require.NoError(t, clearExporter.ConsumeLogs(context.Background(), clearLogs))
	assertRequest([]string{"0"}, []string{""})
}

func TestLogsExporterPreservesMultiResourceScopeRecordBatches(t *testing.T) {
	received := make(chan eventPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = false
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

	logsExporter := startLogsExporter(t, cfg)
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), multiResourceScopeLogs()))

	select {
	case payload := <-received:
		require.Len(t, payload.Records, 4)
		require.ElementsMatch(t, []string{
			"checkout|host-a|checkout.cpu",
			"checkout|host-a|checkout.memory",
			"payments|host-b|payments.latency",
			"payments|host-b|payments.errors",
		}, recordMessageKeys(payload.Records))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fake ServiceNow endpoint")
	}
}

func TestLogsExporterDoesNotPostEmptyLogs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = false

	logsExporter := startLogsExporter(t, cfg)
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), plog.NewLogs()))
	require.Equal(t, int32(0), calls.Load())
}

func TestLogsExporterRetriesRetryableHTTPStatus(t *testing.T) {
	var attempts atomic.Int32
	received := make(chan eventPayload, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		received <- payload

		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err := w.Write([]byte(`{"error":{"code":"rate_limited"}}`))
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.InitialInterval = time.Millisecond
	cfg.BackOffConfig.MaxInterval = time.Millisecond
	cfg.BackOffConfig.MaxElapsedTime = time.Second
	cfg.BackOffConfig.RandomizationFactor = 0
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

	logsExporter, err := createLogsExporter(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	defer func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, logsExporter.ConsumeLogs(ctx, sampleLogs()))
	require.Equal(t, int32(2), attempts.Load())

	for i := 0; i < 2; i++ {
		select {
		case payload := <-received:
			require.Len(t, payload.Records, 1)
			require.Equal(t, "checkout|host-01|request.error_rate", payload.Records[0].MessageKey)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for retry attempt")
		}
	}
}

func TestFactoryRetryAfterOverflowHonorsFiniteRetryBudget(t *testing.T) {
	tests := []struct {
		name                       string
		status                     int
		retryAfter                 string
		needs64BitDurationSaturate bool
		alwaysExceedsBudget        bool
	}{
		{name: "429 first overflow", status: http.StatusTooManyRequests, retryAfter: "9223372037", needs64BitDurationSaturate: true},
		{name: "503 first overflow", status: http.StatusServiceUnavailable, retryAfter: "9223372037", needs64BitDurationSaturate: true},
		{name: "429 former zero wrap", status: http.StatusTooManyRequests, retryAfter: "36028797018963968", needs64BitDurationSaturate: true},
		{name: "503 former zero wrap", status: http.StatusServiceUnavailable, retryAfter: "36028797018963968", needs64BitDurationSaturate: true},
		{name: "ordinary long delay", status: http.StatusTooManyRequests, retryAfter: "30", alwaysExceedsBudget: true},
		{name: "valid zero delay", status: http.StatusTooManyRequests, retryAfter: "0"},
		{name: "invalid delay", status: http.StatusTooManyRequests, retryAfter: "soon"},
		{name: "other retryable status ignores throttle", status: http.StatusInternalServerError, retryAfter: "9223372037"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("Retry-After", tt.retryAfter)
					w.WriteHeader(tt.status)
					return
				}
				// A second request would succeed, making an early retry visible.
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = server.URL
			cfg.Mode = ModeMID
			cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
			cfg.BackOffConfig.Enabled = true
			cfg.BackOffConfig.InitialInterval = time.Millisecond
			cfg.BackOffConfig.MaxInterval = time.Millisecond
			cfg.BackOffConfig.MaxElapsedTime = 5 * time.Second
			cfg.BackOffConfig.RandomizationFactor = 0

			logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
			require.NoError(t, err)
			require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
			defer func() {
				require.NoError(t, logsExporter.Shutdown(context.Background()))
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			logs := sampleLogs()
			logs.MarkReadOnly()
			err = logsExporter.ConsumeLogs(ctx, logs)
			require.NoError(t, ctx.Err())

			budgetStop := tt.alwaysExceedsBudget || (tt.needs64BitDurationSaturate && strconv.IntSize == 64)
			if budgetStop {
				require.Error(t, err)
				require.Contains(t, err.Error(), "no more retries left")
				require.Contains(t, err.Error(), "status="+strconv.Itoa(tt.status))
				require.False(t, consumererror.IsPermanent(err))
				require.Equal(t, int32(1), attempts.Load())
				return
			}

			require.NoError(t, err)
			require.Equal(t, int32(2), attempts.Load())
		})
	}
}

func TestFactoryRetriesUntimedRecordWithStableAdmissionTime(t *testing.T) {
	for _, queueMode := range []struct {
		name     string
		queueOff bool
	}{
		{name: "default queue"},
		{name: "queue disabled", queueOff: true},
	} {
		t.Run(queueMode.name, func(t *testing.T) {
			var attempts atomic.Int32
			received := make(chan []byte, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "could not read request", http.StatusInternalServerError)
					return
				}
				attempt := attempts.Add(1)
				received <- body
				if attempt == 1 {
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = server.URL
			cfg.Mode = ModeMID
			if queueMode.queueOff {
				cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
			}
			cfg.BackOffConfig.InitialInterval = time.Millisecond
			cfg.BackOffConfig.MaxInterval = time.Millisecond
			cfg.BackOffConfig.MaxElapsedTime = 5 * time.Second
			cfg.BackOffConfig.RandomizationFactor = 0
			cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

			logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
			require.NoError(t, err)
			require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
			defer func() {
				require.NoError(t, logsExporter.Shutdown(context.Background()))
			}()

			logs := mixedAdmissionTimestampLogs()
			before := snapshotLogsForTest(t, logs)
			logs.MarkReadOnly()
			require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))

			var first []byte
			for i := 0; i < 2; i++ {
				select {
				case body := <-received:
					if i == 0 {
						first = body
					} else {
						require.Equal(t, first, body, "a retry must reuse the admitted event time")
					}
				case <-time.After(7 * time.Second):
					t.Fatalf("timed out waiting for retry; attempts=%d", attempts.Load())
				}
			}
			require.Equal(t, int32(2), attempts.Load())
			require.Equal(t, before, snapshotLogsForTest(t, logs))

			var payload eventPayload
			require.NoError(t, json.Unmarshal(first, &payload))
			require.Len(t, payload.Records, 4)
			require.Equal(t, []string{
				"explicit.untimed",
				"explicit.event",
				"explicit.observed",
				"explicit.both",
			}, recordMessageKeys(payload.Records))
			require.NotEmpty(t, payload.Records[0].TimeOfEvent)
			require.Equal(t, "2026-05-03 18:34:21", payload.Records[1].TimeOfEvent)
			require.Equal(t, "2026-05-04 18:34:22", payload.Records[2].TimeOfEvent)
			require.Equal(t, "2026-05-05 18:34:23", payload.Records[3].TimeOfEvent, "event timestamp takes precedence when both are supplied")
		})
	}
}

func mixedAdmissionTimestampLogs() plog.Logs {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("service.name", "checkout")
	resourceLogs.Resource().Attributes().PutStr("host.name", "host-01")
	logRecords := resourceLogs.ScopeLogs().AppendEmpty().LogRecords()
	appendAdmissionTimestampRecord(logRecords, "untimed", "explicit.untimed", time.Time{}, time.Time{})
	appendAdmissionTimestampRecord(logRecords, "event", "explicit.event", time.Date(2026, 5, 3, 18, 34, 21, 0, time.UTC), time.Time{})
	appendAdmissionTimestampRecord(logRecords, "observed", "explicit.observed", time.Time{}, time.Date(2026, 5, 4, 18, 34, 22, 0, time.UTC))
	appendAdmissionTimestampRecord(logRecords, "both", "explicit.both", time.Date(2026, 5, 5, 18, 34, 23, 0, time.UTC), time.Date(2026, 5, 6, 18, 34, 24, 0, time.UTC))
	return logs
}

func appendAdmissionTimestampRecord(records plog.LogRecordSlice, name, messageKey string, timestamp, observedTimestamp time.Time) {
	logRecord := records.AppendEmpty()
	if !timestamp.IsZero() {
		logRecord.SetTimestamp(pcommon.NewTimestampFromTime(timestamp))
	}
	if !observedTimestamp.IsZero() {
		logRecord.SetObservedTimestamp(pcommon.NewTimestampFromTime(observedTimestamp))
	}
	logRecord.SetSeverityNumber(plog.SeverityNumberError)
	logRecord.SetSeverityText("ERROR")
	logRecord.Body().SetStr(name)
	logRecord.Attributes().PutStr("event.name", "retry."+name)
	logRecord.Attributes().PutStr("servicenow.message_key", messageKey)
	logRecord.Attributes().PutInt("servicenow.severity", 2)
}

func TestFactoryRetriesTruncatedSuccessfulResponse(t *testing.T) {
	var attempts atomic.Int32
	received := make(chan []byte, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		attempt := attempts.Add(1)
		received <- body
		if attempt == 1 {
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

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.InitialInterval = time.Millisecond
	cfg.BackOffConfig.MaxInterval = time.Millisecond
	cfg.BackOffConfig.MaxElapsedTime = time.Second
	cfg.BackOffConfig.RandomizationFactor = 0

	logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.NoError(t, err)
	require.False(t, logsExporter.Capabilities().MutatesData)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	defer func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	}()

	logs := sampleLogs()
	before := snapshotLogsForTest(t, logs)
	logs.MarkReadOnly()
	err = logsExporter.ConsumeLogs(context.Background(), logs)
	if err != nil {
		require.FailNowf(t, "truncated 2xx acknowledgment should recover on retry", "attempts=%d permanent=%t error=%s", attempts.Load(), consumererror.IsPermanent(err), err)
	}
	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, before, snapshotLogsForTest(t, logs))

	var first []byte
	for i := 0; i < 2; i++ {
		select {
		case body := <-received:
			if i == 0 {
				first = body
			} else {
				require.Equal(t, first, body)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for truncated-response retry")
		}
	}
}

func TestFactoryRetriesUntimedTruncatedSuccessfulResponse(t *testing.T) {
	var attempts atomic.Int32
	received := make(chan []byte, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		attempt := attempts.Add(1)
		received <- body
		if attempt == 1 {
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

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.InitialInterval = 1200 * time.Millisecond
	cfg.BackOffConfig.MaxInterval = 1200 * time.Millisecond
	cfg.BackOffConfig.MaxElapsedTime = 5 * time.Second
	cfg.BackOffConfig.RandomizationFactor = 0

	logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	defer func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	}()

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetTimestamp(0)
	logRecord.SetObservedTimestamp(0)
	before := snapshotLogsForTest(t, logs)
	logs.MarkReadOnly()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))
	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, before, snapshotLogsForTest(t, logs))

	var first []byte
	for i := 0; i < 2; i++ {
		select {
		case body := <-received:
			if i == 0 {
				first = body
			} else {
				require.Equal(t, first, body, "an interrupted 2xx retry must reuse the admitted event time")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for interrupted-acknowledgment retry")
		}
	}
	var payload eventPayload
	require.NoError(t, json.Unmarshal(first, &payload))
	require.Len(t, payload.Records, 1)
	require.NotEmpty(t, payload.Records[0].TimeOfEvent)
}

func TestLogsExporterSurfacesServiceNow2xxRecordFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Len(t, payload.Records, 1)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"records": [
				{
					"__status":"failure",
					"__error":{"message":"Invalid Insert into: em_event","reason":"customer payload"}
				}
			]
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = false
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

	logsExporter := startLogsExporter(t, cfg)
	err := logsExporter.ConsumeLogs(context.Background(), sampleLogs())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed_records=1")
}

func TestLogsExporterQueueBatchingCapsServiceNowRequestSize(t *testing.T) {
	receivedSizes := make(chan int, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		receivedSizes <- len(payload.Records)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = queueBatchingSettings(100, 100, 500*time.Millisecond)
	cfg.BackOffConfig.Enabled = false

	logsExporter := startLogsExporter(t, cfg)

	for i := range 5 {
		require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logsWithRecords(50, i*50)))
	}

	sizes := receiveRequestSizes(t, receivedSizes, 3)
	require.ElementsMatch(t, []int{100, 100, 50}, sizes)
	for _, size := range sizes {
		require.LessOrEqual(t, size, 100)
	}
}

func TestLogsExporterQueueBatchingCanMergeBeyondProcessorCapWithoutMaxSize(t *testing.T) {
	receivedSizes := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		receivedSizes <- len(payload.Records)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = queueBatchingSettings(120, 0, time.Second)
	cfg.BackOffConfig.Enabled = false

	logsExporter := startLogsExporter(t, cfg)

	for i := range 3 {
		require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logsWithRecords(40, i*40)))
	}

	sizes := receiveRequestSizes(t, receivedSizes, 1)
	require.Equal(t, []int{120}, sizes)
}

func TestFactoryMetadataOverflowSendsCompleteReadOnlyCallback(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		limit                 int
		severityText          string
		configuredIdentity    bool
		wantSeverityText      string
		wantDroppedAttributes string
	}{
		{
			name:                  "default budget plain severity",
			limit:                 4000,
			severityText:          strings.Repeat("x", 6000),
			wantDroppedAttributes: "3",
		},
		{
			name:                  "default budget escaped severity",
			limit:                 4000,
			severityText:          strings.Repeat("escaped\"\\\n\u0001λ", 400),
			wantDroppedAttributes: "3",
		},
		{
			name:                  "minimum budget missing identity and optional value",
			limit:                 256,
			configuredIdentity:    true,
			wantSeverityText:      "ERROR",
			wantDroppedAttributes: "4",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			received := make(chan eventPayload, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload eventPayload
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				requests.Add(1)
				received <- payload
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = server.URL
			cfg.Mode = ModeMID
			cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
			cfg.FieldLimits.AdditionalInfo = tt.limit
			cfg.BackOffConfig.Enabled = true
			if tt.configuredIdentity {
				cfg.MessageKey.Attributes = []string{"service.name", "deployment.environment"}
			}
			logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
			require.NoError(t, err)
			require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
			t.Cleanup(func() { require.NoError(t, logsExporter.Shutdown(context.Background())) })

			logs := logsWithRecords(100, 0)
			logRecords := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
			for i := 0; i < 99; i++ {
				if tt.configuredIdentity {
					logRecords.At(i).Attributes().PutStr("deployment.environment", "prod")
				}
			}
			last := logRecords.At(99)
			if tt.severityText != "" {
				last.SetSeverityText(tt.severityText)
			}
			if tt.configuredIdentity {
				last.Attributes().PutStr("optional.value", strings.Repeat("x", 40))
			}
			last.Attributes().PutInt("servicenow.severity", 0)
			before := snapshotLogsForTest(t, logs)
			logs.MarkReadOnly()

			require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))
			select {
			case payload := <-received:
				require.Len(t, payload.Records, 100)
				require.Equal(t, int32(1), requests.Load())
				for i := 0; i < 99; i++ {
					wantKey := fmt.Sprintf("opentelemetry|host-01|otel-log|checkout|queue.batch.%03d", i)
					if tt.configuredIdentity {
						wantKey = "checkout|prod"
					}
					require.Equal(t, wantKey, payload.Records[i].MessageKey)
					require.Equal(t, fmt.Sprintf("queue.batch.%03d", i), payload.Records[i].MetricName)
					require.Equal(t, "2", payload.Records[i].Severity)
					require.Empty(t, payload.Records[i].ResolutionState)
					require.NotContains(t, requireAdditionalInfo(t, payload.Records[i]), "otel.servicenow.additional_info.metadata_reduced")
				}
				wantLastKey := "opentelemetry|host-01|otel-log|checkout|queue.batch.099"
				require.Equal(t, wantLastKey, payload.Records[99].MessageKey)
				require.Equal(t, "0", payload.Records[99].Severity)
				require.Equal(t, serviceNowResolutionStateClosing, payload.Records[99].ResolutionState)
				require.LessOrEqual(t, len(payload.Records[99].AdditionalInfo), tt.limit)
				lastInfo := requireAdditionalInfo(t, payload.Records[99])
				require.Equal(t, "true", lastInfo["otel.servicenow.additional_info.metadata_reduced"])
				require.Equal(t, "17", lastInfo["otel.severity_number"])
				require.Equal(t, tt.wantDroppedAttributes, lastInfo["otel.servicenow.additional_info.dropped_attributes"])
				if tt.wantSeverityText == "" {
					require.NotContains(t, lastInfo, "otel.severity_text")
				} else {
					require.Equal(t, tt.wantSeverityText, lastInfo["otel.severity_text"])
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for the factory exporter request")
			}
			require.Equal(t, before, snapshotLogsForTest(t, logs))
		})
	}
}

func TestFactoryMetadataOverflowQueueDrainKeepsBothRequests(t *testing.T) {
	var requests atomic.Int32
	received := make(chan eventPayload, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		requests.Add(1)
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.FieldLimits.AdditionalInfo = 256
	queue := exporterhelper.NewDefaultQueueConfig()
	queue.NumConsumers = 1
	queue.Batch = configoptional.None[exporterhelper.BatchConfig]()
	cfg.QueueSettings = configoptional.Some(queue)
	cfg.BackOffConfig.Enabled = true

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

	overflow := logsWithRecords(100, 0)
	overflow.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(99).SetSeverityText(strings.Repeat("x", 1024))
	healthy := logsWithRecords(1, 100)
	overflowBefore := snapshotLogsForTest(t, overflow)
	healthyBefore := snapshotLogsForTest(t, healthy)
	overflow.MarkReadOnly()
	healthy.MarkReadOnly()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), overflow))
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), healthy))
	require.NoError(t, logsExporter.Shutdown(context.Background()))
	shutdown = true

	require.Equal(t, int32(2), requests.Load())
	require.Len(t, received, 2)
	first := <-received
	second := <-received
	require.Len(t, first.Records, 100)
	for i, record := range first.Records {
		require.Equal(t, fmt.Sprintf("queue.batch.%03d", i), record.MetricName)
		require.Equal(t, fmt.Sprintf("opentelemetry|host-01|otel-log|checkout|queue.batch.%03d", i), record.MessageKey)
		require.Equal(t, "2", record.Severity)
		if i < 99 {
			require.NotContains(t, requireAdditionalInfo(t, record), "otel.servicenow.additional_info.metadata_reduced")
		}
	}
	require.Equal(t, "true", requireAdditionalInfo(t, first.Records[99])["otel.servicenow.additional_info.metadata_reduced"])
	require.Len(t, second.Records, 1)
	require.Equal(t, "queue.batch.100", second.Records[0].MetricName)
	require.Equal(t, "opentelemetry|host-01|otel-log|checkout|queue.batch.100", second.Records[0].MessageKey)
	require.Empty(t, observed.FilterLevelExact(zapcore.ErrorLevel).All(), "queue drain should not log a permanent drop")
	require.Equal(t, overflowBefore, snapshotLogsForTest(t, overflow))
	require.Equal(t, healthyBefore, snapshotLogsForTest(t, healthy))
}

func snapshotLogsForTest(t *testing.T, logs plog.Logs) string {
	t.Helper()
	encoded, err := (&plog.JSONMarshaler{}).MarshalLogs(logs)
	require.NoError(t, err)
	return string(encoded)
}

func queueBatchingSettings(minSize, maxSize int64, flushTimeout time.Duration) configoptional.Optional[exporterhelper.QueueBatchConfig] {
	return configoptional.Some(exporterhelper.QueueBatchConfig{
		Sizer:        exporterhelper.RequestSizerTypeRequests,
		QueueSize:    1_000,
		NumConsumers: 1,
		Batch: configoptional.Some(exporterhelper.BatchConfig{
			FlushTimeout: flushTimeout,
			Sizer:        exporterhelper.RequestSizerTypeItems,
			MinSize:      minSize,
			MaxSize:      maxSize,
		}),
	})
}

func receiveRequestSizes(t *testing.T, received <-chan int, count int) []int {
	t.Helper()

	sizes := make([]int, 0, count)
	for len(sizes) < count {
		select {
		case size := <-received:
			sizes = append(sizes, size)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %d ServiceNow requests, received %d", count, len(sizes))
		}
	}
	return sizes
}

func logsWithRecords(count, offset int) plog.Logs {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("service.name", "checkout")
	resourceLogs.Resource().Attributes().PutStr("host.name", "host-01")

	logRecords := resourceLogs.ScopeLogs().AppendEmpty().LogRecords()
	for i := range count {
		sequence := offset + i
		logRecord := logRecords.AppendEmpty()
		logRecord.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 22, 12, 0, sequence, 0, time.UTC)))
		logRecord.SetSeverityNumber(plog.SeverityNumberError)
		logRecord.SetSeverityText("ERROR")
		logRecord.Body().SetStr(fmt.Sprintf("queue batch event %03d", sequence))
		logRecord.Attributes().PutStr("event.name", fmt.Sprintf("queue.batch.%03d", sequence))
		logRecord.Attributes().PutInt("servicenow.severity", 2)
	}
	return logs
}

func multiResourceScopeLogs() plog.Logs {
	logs := plog.NewLogs()
	appendScopedLog(logs, "checkout", "host-a", "checkout.cpu", 0)
	appendScopedLog(logs, "checkout", "host-a", "checkout.memory", 1)
	appendScopedLog(logs, "payments", "host-b", "payments.latency", 2)
	appendScopedLog(logs, "payments", "host-b", "payments.errors", 3)
	return logs
}

func appendScopedLog(logs plog.Logs, serviceName, hostName, eventName string, offset int) {
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("service.name", serviceName)
	resourceLogs.Resource().Attributes().PutStr("host.name", hostName)

	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
	scopeLogs.Scope().SetName("scope." + eventName)
	logRecord := scopeLogs.LogRecords().AppendEmpty()
	logRecord.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 22, 13, 0, offset, 0, time.UTC)))
	logRecord.SetSeverityNumber(plog.SeverityNumberError)
	logRecord.SetSeverityText("ERROR")
	logRecord.Body().SetStr("event " + eventName)
	logRecord.Attributes().PutStr("event.name", eventName)
	logRecord.Attributes().PutInt("servicenow.severity", 2)
}

func recordMessageKeys(records []eventRecord) []string {
	keys := make([]string, 0, len(records))
	for _, record := range records {
		keys = append(keys, record.MessageKey)
	}
	return keys
}
