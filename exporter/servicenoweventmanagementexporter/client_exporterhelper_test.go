// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
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
