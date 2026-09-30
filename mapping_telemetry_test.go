// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const mappingDiagnosticsMetricName = "otelcol_exporter_servicenow_event_management_mapping_diagnostics"

type mappingDiagnosticMeasurement struct {
	reason   string
	exporter string
	value    int64
}

func TestFactoryRecordsMappingDiagnosticCounter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	telemetry := componenttest.NewTelemetry()
	settings := exportertest.NewNopSettings(Type)
	settings.TelemetrySettings = telemetry.NewTelemetrySettings()
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.BackOffConfig.Enabled = false

	logsExporter, err := NewFactory().CreateLogs(context.Background(), settings, cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	t.Cleanup(func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	})

	logs := sampleLogs()
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("servicenow.severity", "not-a-severity")
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))

	metric, err := telemetry.GetMetric(mappingDiagnosticsMetricName)
	require.NoError(t, err)
	require.Equal(t, "{record}", metric.Unit)
	require.Contains(t, metric.Description, "Affected log-record occurrences per mapping attempt, by reason")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.True(t, sum.IsMonotonic)
	require.Equal(t, []mappingDiagnosticMeasurement{{
		reason:   "invalid_severity_override",
		exporter: settings.ID.String(),
		value:    1,
	}}, measurementsFromSum(t, sum))
}

func TestFactoryMappingDiagnosticsAreIndependentOfHTTPOutcome(t *testing.T) {
	var payloads []string
	for _, tt := range []struct {
		name       string
		statusCode int
		wantError  bool
	}{
		{name: "accepted", statusCode: http.StatusAccepted},
		{name: "service unavailable", statusCode: http.StatusServiceUnavailable, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requestBodies := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				requestBodies <- string(body)
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			telemetry := componenttest.NewTelemetry()
			cfg := noQueueTestConfig(server.URL)
			cfg.BackOffConfig.Enabled = false
			settings := exportertest.NewNopSettings(Type)
			logsExporter, _ := startTelemetryTestExporter(t, settings, telemetry, cfg)
			err := logsExporter.ConsumeLogs(context.Background(), logsWithInvalidSeverity())
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			requestBody := <-requestBodies

			require.Equal(t, []mappingDiagnosticMeasurement{{
				reason:   "invalid_severity_override",
				exporter: settings.ID.String(),
				value:    1,
			}}, readMappingMeasurements(t, telemetry))
			if tt.statusCode == http.StatusAccepted {
				require.Equal(t, int64(1), readCounterTotal(t, telemetry, "otelcol_exporter_sent_log_records"))
				require.Equal(t, int64(0), readCounterTotal(t, telemetry, "otelcol_exporter_send_failed_log_records"))
			} else {
				require.Equal(t, int64(0), readCounterTotal(t, telemetry, "otelcol_exporter_sent_log_records"))
				require.Equal(t, int64(1), readCounterTotal(t, telemetry, "otelcol_exporter_send_failed_log_records"))
			}
			payloads = append(payloads, requestBody)
		})
	}
	require.Len(t, payloads, 2)
	require.Equal(t, payloads[0], payloads[1], "HTTP outcome must not change the mapped payload")
}

func TestFactoryEmitsOnlyBoundedReasonAndExporterAttributes(t *testing.T) {
	requestBodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body", http.StatusInternalServerError)
			return
		}
		requestBodies <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	telemetry := componenttest.NewTelemetry()
	settings := exportertest.NewNopSettings(Type)
	cfg := allDiagnosticTestConfig(server.URL)
	logsExporter, _ := startTelemetryTestExporter(t, settings, telemetry, cfg)
	logs := logsWithAllDiagnostics()
	attrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes()
	attrs.PutStr("reason", "not-a-real-reason")
	attrs.PutStr("exporter", "input-sentinel")
	attrs.PutStr(additionalInfoDroppedAttributesKey, "0")
	attrs.PutStr(additionalInfoMetadataReducedKey, "false")
	attrs.PutStr(additionalInfoDroppedByCountKey, "0")
	attrs.PutStr(additionalInfoDroppedByValueKey, "0")
	attrs.PutStr(additionalInfoRedactedAttributesKey, "0")
	attrs.PutStr(additionalInfoTruncatedValuesKey, "0")
	attrs.PutStr(truncatedFieldsKey, "")
	attrs.PutStr(messageKeyMissingAttributesKey, "")
	attrs.PutStr(messageKeyStrategyKey, "not-fallback")
	attrs.PutStr("otel.other", "private-input-sentinel")
	before := snapshotLogsForTest(t, logs)
	logs.MarkReadOnly()
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logs))
	require.Equal(t, before, snapshotLogsForTest(t, logs), "factory mapping must leave caller pdata unchanged")
	var payload eventPayload
	require.NoError(t, json.Unmarshal(<-requestBodies, &payload))
	require.Len(t, payload.Records, 1)
	require.LessOrEqual(t, len(payload.Records[0].AdditionalInfo), 256)

	metric, err := telemetry.GetMetric(mappingDiagnosticsMetricName)
	require.NoError(t, err)
	require.NotContains(t, metric.Description, "input-sentinel")
	require.NotContains(t, metric.Description, "private-input-sentinel")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	measurements := measurementsFromSum(t, sum)
	require.Len(t, measurements, int(mappingDiagnosticReasonCount))
	wantReasons := make(map[string]bool, mappingDiagnosticReasonCount)
	allowedReasons := []string{
		"invalid_severity_override",
		"configured_message_key_fallback",
		"event_field_truncated",
		"additional_info_value_truncated",
		"additional_info_attribute_dropped",
		"additional_info_metadata_reduced",
	}
	require.Equal(t, allowedReasons, mappingDiagnosticReasonNames[:])
	for _, reason := range allowedReasons {
		wantReasons[reason] = true
	}
	for _, measurement := range measurements {
		require.True(t, wantReasons[measurement.reason], "unexpected reason label %q", measurement.reason)
		require.Equal(t, settings.ID.String(), measurement.exporter)
		require.Equal(t, int64(1), measurement.value)
		require.NotContains(t, measurement.reason, "sentinel")
		require.NotContains(t, measurement.exporter, "sentinel")
	}
}

func TestFactoryPreservesFirstMappingErrorObservations(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	telemetry := componenttest.NewTelemetry()
	settings := exportertest.NewNopSettings(Type)
	cfg := allDiagnosticTestConfig(server.URL)
	cfg.BackOffConfig.Enabled = true
	logsExporter, _ := startTelemetryTestExporter(t, settings, telemetry, cfg)
	logs := logsWithAllDiagnostics()
	logRecords := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	oversized := logRecords.AppendEmpty()
	oversized.Attributes().PutStr("servicenow.message_key", strings.Repeat("private-key-", 9))
	oversized.Attributes().PutStr("servicenow.severity", "invalid-second-record")
	third := logRecords.AppendEmpty()
	third.Attributes().PutStr("servicenow.severity", "invalid-third-record")

	err := logsExporter.ConsumeLogs(context.Background(), logs)
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "explicit message_key exceeds configured byte limit")
	require.NotContains(t, err.Error(), "private-key")
	require.NotContains(t, err.Error(), "invalid-second-record")
	require.NotContains(t, err.Error(), "invalid-third-record")
	require.Equal(t, int32(0), requests.Load())
	measurements := readMappingMeasurements(t, telemetry)
	require.Len(t, measurements, int(mappingDiagnosticReasonCount))
	for _, measurement := range measurements {
		require.Equal(t, settings.ID.String(), measurement.exporter)
		require.Equal(t, int64(1), measurement.value, measurement.reason)
	}
	require.Equal(t, int64(3), readCounterTotal(t, telemetry, "otelcol_exporter_send_failed_log_records"))
}

func TestFactoryRetriesRepeatMappingDiagnostics(t *testing.T) {
	var attempts atomic.Int32
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requests <- string(body)
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	telemetry := componenttest.NewTelemetry()
	cfg := noQueueTestConfig(server.URL)
	cfg.BackOffConfig.Enabled = true
	cfg.BackOffConfig.InitialInterval = time.Millisecond
	cfg.BackOffConfig.MaxInterval = time.Millisecond
	cfg.BackOffConfig.MaxElapsedTime = time.Second
	cfg.BackOffConfig.RandomizationFactor = 0
	settings := exportertest.NewNopSettings(Type)
	logsExporter, _ := startTelemetryTestExporter(t, settings, telemetry, cfg)
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logsWithInvalidSeverity()))
	require.Equal(t, int32(2), attempts.Load())
	firstPayload := <-requests
	secondPayload := <-requests
	require.Equal(t, firstPayload, secondPayload, "each retry must remap the same payload")
	require.Equal(t, []mappingDiagnosticMeasurement{{
		reason:   "invalid_severity_override",
		exporter: settings.ID.String(),
		value:    2,
	}}, readMappingMeasurements(t, telemetry))
	require.Equal(t, int64(1), readCounterTotal(t, telemetry, "otelcol_exporter_sent_log_records"))
	require.Equal(t, int64(0), readCounterTotal(t, telemetry, "otelcol_exporter_send_failed_log_records"))
}

func TestFactoryQueuesBeforeMappingAndFlushesDiagnosticsOnShutdown(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	telemetry := componenttest.NewTelemetry()
	settings := exportertest.NewNopSettings(Type)
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.QueueSettings = queueBatchingSettings(2, 2, time.Hour)
	cfg.BackOffConfig.Enabled = false
	logsExporter, shutdown := startTelemetryTestExporter(t, settings, telemetry, cfg)
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), logsWithInvalidSeverity()))
	require.Empty(t, readMappingMeasurements(t, telemetry), "queue acceptance has not run mapping")
	require.Empty(t, requests)
	shutdown()
	require.Len(t, requests, 1)
	require.Equal(t, []mappingDiagnosticMeasurement{{
		reason:   "invalid_severity_override",
		exporter: settings.ID.String(),
		value:    1,
	}}, readMappingMeasurements(t, telemetry))
}

func TestFactorySeparatesExporterIDsAndConcurrentCallbacks(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	telemetry := componenttest.NewTelemetry()
	settingsA := exportertest.NewNopSettings(Type)
	settingsA.ID = component.NewIDWithName(Type, "telemetry-a")
	settingsB := exportertest.NewNopSettings(Type)
	settingsB.ID = component.NewIDWithName(Type, "telemetry-b")
	exporterA, _ := startTelemetryTestExporter(t, settingsA, telemetry, noQueueTestConfig(server.URL))
	exporterB, _ := startTelemetryTestExporter(t, settingsB, telemetry, noQueueTestConfig(server.URL))
	require.NoError(t, exporterA.ConsumeLogs(context.Background(), logsWithInvalidSeverity()))
	require.NoError(t, exporterB.ConsumeLogs(context.Background(), logsWithInvalidSeverity()))

	const concurrentCallbacks = 12
	var waitGroup sync.WaitGroup
	callbackErrors := make(chan error, concurrentCallbacks)
	for index := 0; index < concurrentCallbacks; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			callbackErrors <- exporterA.ConsumeLogs(context.Background(), logsWithInvalidSeverity())
		}()
	}
	waitGroup.Wait()
	close(callbackErrors)
	for err := range callbackErrors {
		require.NoError(t, err)
	}
	require.Equal(t, int32(concurrentCallbacks+2), requests.Load())

	measurements := readMappingMeasurements(t, telemetry)
	require.Len(t, measurements, 2)
	countsByExporter := make(map[string]int64, 2)
	for _, measurement := range measurements {
		require.Equal(t, "invalid_severity_override", measurement.reason)
		countsByExporter[measurement.exporter] = measurement.value
	}
	require.Equal(t, int64(concurrentCallbacks+1), countsByExporter[settingsA.ID.String()])
	require.Equal(t, int64(1), countsByExporter[settingsB.ID.String()])
}

func TestFactoryReturnsTelemetryConstructionError(t *testing.T) {
	settings := exportertest.NewNopSettings(Type)
	settings.MeterProvider = mappingDiagnosticsFailureMeterProvider{}
	_, err := NewFactory().CreateLogs(context.Background(), settings, noQueueTestConfig("http://127.0.0.1:1"))
	require.ErrorContains(t, err, "mapping counter construction failed")
}

func TestMappingDiagnosticsCountEachReasonOncePerRecordAcrossGroups(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Attributes = []string{"missing." + strings.Repeat("m", 240)}
	cfg.FieldLimits.Source = 3
	cfg.FieldLimits.EventClass = 2
	cfg.FieldLimits.AdditionalInfo = 256
	cfg.AdditionalInfo.IncludeAttributes = []string{"bad.*", "count.*", "value.*"}
	cfg.AdditionalInfo.MaxAttributes = 2
	cfg.AdditionalInfo.MaxValueLength = 3

	logs := plog.NewLogs()
	for resourceIndex := 0; resourceIndex < 2; resourceIndex++ {
		resourceLogs := logs.ResourceLogs().AppendEmpty()
		resourceAttrs := resourceLogs.Resource().Attributes()
		resourceAttrs.PutStr("servicenow.source", "long-source")
		resourceAttrs.PutStr("servicenow.event_class", "long-class")
		resourceAttrs.PutInt("servicenow.severity", 4)
		for scopeIndex := 0; scopeIndex < 2; scopeIndex++ {
			logRecord := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
			logRecord.SetSeverityNumber(plog.SeverityNumberError)
			logRecord.SetSeverityText(strings.Repeat("S", 1200))
			attrs := logRecord.Attributes()
			attrs.PutStr("servicenow.severity", "bad")
			attrs.PutStr("value.one", "abcdef")
			attrs.PutStr("value.two", "ghijkl")
			attrs.PutStr("count.one", "one")
			attrs.PutStr("count.two", "two")
			badMap := attrs.PutEmptyMap("bad.map")
			badMap.PutDouble("not_json", math.NaN())
		}
	}
	logs.MarkReadOnly()

	payload, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
	require.NoError(t, err)
	require.Len(t, payload.Records, 4)
	for reason := mappingDiagnosticReason(0); reason < mappingDiagnosticReasonCount; reason++ {
		require.Equal(t, int64(4), summary.counts[reason], mappingDiagnosticReasonNames[reason])
	}
	require.Equal(t, "2", payload.Records[0].Severity, "invalid log severity still shadows the valid resource override")
	require.Equal(t, "lon", payload.Records[0].Source)
	require.Equal(t, "lo", payload.Records[0].EventClass)
	require.LessOrEqual(t, len(payload.Records[0].AdditionalInfo), 256)
	require.Equal(t, "true", requireAdditionalInfo(t, payload.Records[0])[additionalInfoMetadataReducedKey])
	require.NotContains(t, requireAdditionalInfo(t, payload.Records[0]), truncatedFieldsKey, "the bounded payload may omit details while summary keeps observations")
}

func TestMappingDiagnosticsCountReachedResourceCandidateShadowedByLog(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.IncludeAttributes = []string{"shadowed.*"}
	cfg.AdditionalInfo.MaxValueLength = 3
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("shadowed.value", "resource-value")
	logRecord := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	logRecord.Attributes().PutStr("shadowed.value", "ok")

	payload, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
	require.NoError(t, err)
	require.Len(t, payload.Records, 1)
	require.Equal(t, "ok", requireAdditionalInfo(t, payload.Records[0])["shadowed.value"])
	want := mappingDiagnosticSummary{}
	want.counts[mappingDiagnosticAdditionalInfoValueTruncated] = 1
	require.Equal(t, want, summary, "resource truncation remains counted after log-over-resource replacement")
}

func TestMappingDiagnosticsIsolatesAdditionalInfoDecisions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config func(*Config)
		input  func(plog.LogRecord)
		want   mappingDiagnosticReason
	}{
		{
			name: "render failure",
			config: func(cfg *Config) {
				cfg.AdditionalInfo.IncludeAttributes = []string{"bad.*"}
			},
			input: func(record plog.LogRecord) {
				badMap := record.Attributes().PutEmptyMap("bad.map")
				badMap.PutDouble("not_json", math.NaN())
			},
			want: mappingDiagnosticAdditionalInfoAttributeDropped,
		},
		{
			name: "attribute count limit",
			config: func(cfg *Config) {
				cfg.AdditionalInfo.IncludeAttributes = []string{"count.*"}
				cfg.AdditionalInfo.MaxAttributes = 1
			},
			input: func(record plog.LogRecord) {
				record.Attributes().PutStr("count.one", "one")
				record.Attributes().PutStr("count.two", "two")
			},
			want: mappingDiagnosticAdditionalInfoAttributeDropped,
		},
		{
			name: "encoded byte shedding",
			config: func(cfg *Config) {
				cfg.FieldLimits.AdditionalInfo = 256
				cfg.AdditionalInfo.IncludeAttributes = []string{"large.*"}
			},
			input: func(record plog.LogRecord) {
				record.Attributes().PutStr("large.keep", "small")
				record.Attributes().PutStr("large.drop", strings.Repeat("x", 220))
			},
			want: mappingDiagnosticAdditionalInfoAttributeDropped,
		},
		{
			name: "metadata reduction",
			config: func(cfg *Config) {
				cfg.FieldLimits.AdditionalInfo = 256
				cfg.AdditionalInfo.IncludeAttributes = []string{"never"}
			},
			input: func(record plog.LogRecord) {
				record.SetSeverityText(strings.Repeat("long-severity", 100))
			},
			want: mappingDiagnosticAdditionalInfoMetadataReduced,
		},
		{
			name: "value truncation",
			config: func(cfg *Config) {
				cfg.AdditionalInfo.IncludeAttributes = []string{"value.*"}
				cfg.AdditionalInfo.MaxValueLength = 3
			},
			input: func(record plog.LogRecord) {
				record.Attributes().PutStr("value.one", "abcdef")
				record.Attributes().PutStr("value.two", "ghijkl")
			},
			want: mappingDiagnosticAdditionalInfoValueTruncated,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			tt.config(cfg)
			logs := plog.NewLogs()
			logRecord := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
			tt.input(logRecord)
			_, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
			require.NoError(t, err)
			expected := mappingDiagnosticSummary{}
			expected.counts[tt.want] = 1
			require.Equal(t, expected, summary)
		})
	}
}

func TestMappingDiagnosticsDoNotCountOrdinaryOrCollisionOperations(t *testing.T) {
	tests := []struct {
		name   string
		config func(*Config)
		input  func(plog.LogRecord)
	}{
		{name: "ordinary record"},
		{
			name: "complete configured identity",
			config: func(cfg *Config) {
				cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}
			},
		},
		{
			name: "explicit identity",
			input: func(record plog.LogRecord) {
				record.Attributes().PutStr("servicenow.message_key", "fixed-key")
			},
		},
		{
			name: "valid integral severity",
			input: func(record plog.LogRecord) {
				record.Attributes().PutInt("servicenow.severity", 0)
				record.Attributes().PutStr("reason", "invalid_severity_override")
				record.Attributes().PutStr("exporter", "hostile-exporter")
			},
		},
		{
			name: "collision filtering and redaction",
			config: func(cfg *Config) {
				cfg.AdditionalInfo.IncludeAttributes = []string{"otel.signal", "password", "producer.*"}
				cfg.AdditionalInfo.RedactAttributes = []string{"password"}
			},
			input: func(record plog.LogRecord) {
				record.Attributes().PutStr("otel.signal", "producer-sentinel")
				record.Attributes().PutStr("password", "secret-sentinel")
				record.Attributes().PutStr("producer.reason", "event_field_truncated")
				record.Attributes().PutStr("producer.exporter", "sentinel-exporter")
				record.Attributes().PutStr("servicenow.filtered", "control")
			},
		},
		{
			name: "diagnostic-looking producer keys cannot forge counts",
			config: func(cfg *Config) {
				cfg.AdditionalInfo.IncludeAttributes = []string{"exporter", "otel.*", "reason"}
			},
			input: putHostileDiagnosticAttributes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			if tt.config != nil {
				tt.config(cfg)
			}
			logs := sampleLogs()
			if tt.input != nil {
				tt.input(logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0))
			}
			_, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
			require.NoError(t, err)
			require.Equal(t, mappingDiagnosticSummary{}, summary)
		})
	}

	_, emptySummary, err := mapLogsToPayloadWithDiagnostics(plog.NewLogs(), newMappingConfig(createDefaultConfig().(*Config)))
	require.NoError(t, err)
	require.Equal(t, mappingDiagnosticSummary{}, emptySummary)

	t.Run("hostile producer values cannot suppress reached observations", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Attributes = []string{"missing.actual"}
		cfg.AdditionalInfo.IncludeAttributes = []string{"exporter", "otel.*", "reason"}
		logs := logsWithInvalidSeverity()
		putHostileDiagnosticAttributes(logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0))
		_, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
		require.NoError(t, err)
		want := mappingDiagnosticSummary{}
		want.counts[mappingDiagnosticInvalidSeverityOverride] = 1
		want.counts[mappingDiagnosticConfiguredMessageKeyFallback] = 1
		require.Equal(t, want, summary)
	})
}

func TestMappingDiagnosticsIgnoreProducerMetadataCollisions(t *testing.T) {
	metadataKeys := hostileMetadataKeys()

	t.Run("collision-only fitting does not count as a dropped attribute", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.AdditionalInfo.IncludeAttributes = metadataKeys
		cfg.FieldLimits.AdditionalInfo = 4096
		logs := plog.NewLogs()
		logRecord := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		putHostileDiagnosticAttributes(logRecord)

		payload, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
		require.NoError(t, err)
		require.Len(t, payload.Records, 1)
		require.Equal(t, mappingDiagnosticSummary{}, summary)
		info := requireAdditionalInfo(t, payload.Records[0])
		require.Equal(t, "1", info[additionalInfoDroppedAttributesKey], "the existing payload collision accounting remains independent of diagnostic flags")
		require.NotContains(t, info, additionalInfoMetadataReducedKey)
	})

	t.Run("producer values cannot forge or suppress reached observations", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Attributes = []string{"missing.actual"}
		cfg.FieldLimits.Source = 3
		cfg.FieldLimits.AdditionalInfo = 4096
		cfg.AdditionalInfo.IncludeAttributes = append(metadataKeys, "value.actual")
		cfg.AdditionalInfo.MaxValueLength = 3
		logs := plog.NewLogs()
		resourceLogs := logs.ResourceLogs().AppendEmpty()
		resourceLogs.Resource().Attributes().PutStr("servicenow.source", "long-source")
		logRecord := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		logRecord.Attributes().PutStr("value.actual", "abcdef")
		putHostileDiagnosticAttributes(logRecord)
		for _, key := range metadataKeys {
			logRecord.Attributes().PutStr(key, "x")
		}

		payload, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
		require.NoError(t, err)
		require.Len(t, payload.Records, 1)
		want := mappingDiagnosticSummary{}
		want.counts[mappingDiagnosticConfiguredMessageKeyFallback] = 1
		want.counts[mappingDiagnosticEventFieldTruncated] = 1
		want.counts[mappingDiagnosticAdditionalInfoValueTruncated] = 1
		require.Equal(t, want, summary)

		info := requireAdditionalInfo(t, payload.Records[0])
		require.Equal(t, messageKeyStrategyFallbackDefaultFields, info[messageKeyStrategyKey])
		require.Equal(t, "missing.actual", info[messageKeyMissingAttributesKey])
		require.Equal(t, "source", info[truncatedFieldsKey])
		require.Equal(t, "1", info[additionalInfoTruncatedValuesKey])
		require.Equal(t, "abc", info["value.actual"])
	})
}

func putHostileDiagnosticAttributes(record plog.LogRecord) {
	attrs := record.Attributes()
	attrs.PutStr("reason", "invalid_severity_override")
	attrs.PutStr("exporter", "forged-exporter-id")
	attrs.PutStr("otel.signal", "logs")
	attrs.PutStr(additionalInfoDroppedAttributesKey, "0")
	attrs.PutStr(additionalInfoMetadataReducedKey, "true")
	attrs.PutStr(additionalInfoDroppedByCountKey, "0")
	attrs.PutStr(additionalInfoDroppedByValueKey, "0")
	attrs.PutStr(additionalInfoRedactedAttributesKey, "999")
	attrs.PutStr(additionalInfoTruncatedValuesKey, "0")
	attrs.PutStr(truncatedFieldsKey, "source")
	attrs.PutStr(messageKeyMissingAttributesKey, "fake.missing")
	attrs.PutStr(messageKeyStrategyKey, messageKeyStrategyFallbackDefaultFields)
}

func hostileMetadataKeys() []string {
	return []string{
		additionalInfoDroppedAttributesKey,
		additionalInfoMetadataReducedKey,
		additionalInfoDroppedByCountKey,
		additionalInfoDroppedByValueKey,
		additionalInfoRedactedAttributesKey,
		additionalInfoTruncatedValuesKey,
		truncatedFieldsKey,
		messageKeyMissingAttributesKey,
		messageKeyStrategyKey,
	}
}

func TestMappingDiagnosticsRespectConfiguredFallbackAndSelectedSeverity(t *testing.T) {
	for _, format := range []string{messageKeyFormatLegacy, messageKeyFormatSHA256V1} {
		t.Run(format, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.MessageKey.Format = format
			cfg.MessageKey.Attributes = []string{"missing.identity"}
			logs := sampleLogs()
			payload, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
			require.NoError(t, err)
			require.Len(t, payload.Records, 1)
			if format == messageKeyFormatLegacy {
				require.Equal(t, "opentelemetry|host-01|application|checkout|request.error_rate", payload.Records[0].MessageKey)
			} else {
				require.True(t, strings.HasPrefix(payload.Records[0].MessageKey, messageKeySHA256V1Prefix))
				require.Len(t, payload.Records[0].MessageKey, 79)
			}
			want := mappingDiagnosticSummary{}
			want.counts[mappingDiagnosticConfiguredMessageKeyFallback] = 1
			require.Equal(t, want, summary)
		})
	}

	t.Run("invalid log override shadows valid resource override", func(t *testing.T) {
		logs := sampleLogs()
		resource := logs.ResourceLogs().At(0).Resource().Attributes()
		resource.PutInt("servicenow.severity", 4)
		_, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(createDefaultConfig().(*Config)))
		require.NoError(t, err)
		require.True(t, summary.counts[mappingDiagnosticInvalidSeverityOverride] == 0, "valid log override wins over resource override")
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("servicenow.severity", "invalid")
		_, summary, err = mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(createDefaultConfig().(*Config)))
		require.NoError(t, err)
		require.Equal(t, int64(1), summary.counts[mappingDiagnosticInvalidSeverityOverride])
	})

	t.Run("invalid resource override is counted", func(t *testing.T) {
		logs := sampleLogs()
		logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
		logRecord.Attributes().Remove("servicenow.severity")
		logs.ResourceLogs().At(0).Resource().Attributes().PutStr("servicenow.severity", "invalid")
		_, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(createDefaultConfig().(*Config)))
		require.NoError(t, err)
		require.Equal(t, int64(1), summary.counts[mappingDiagnosticInvalidSeverityOverride])
	})

	t.Run("integral double clear keeps closing state without diagnostic", func(t *testing.T) {
		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutDouble("servicenow.severity", 0)
		payload, summary, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(createDefaultConfig().(*Config)))
		require.NoError(t, err)
		require.Equal(t, "0", payload.Records[0].Severity)
		require.Equal(t, serviceNowResolutionStateClosing, payload.Records[0].ResolutionState)
		require.Equal(t, mappingDiagnosticSummary{}, summary)
	})
}

func noQueueTestConfig(endpoint string) *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = endpoint
	cfg.QueueSettings = configoptional.None[exporterhelper.QueueBatchConfig]()
	return cfg
}

func allDiagnosticTestConfig(endpoint string) *Config {
	cfg := noQueueTestConfig(endpoint)
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.MessageKey.Attributes = []string{"missing." + strings.Repeat("m", 240)}
	cfg.FieldLimits.MessageKey = 79
	cfg.FieldLimits.Source = 3
	cfg.FieldLimits.EventClass = 2
	cfg.FieldLimits.AdditionalInfo = 256
	cfg.AdditionalInfo.IncludeAttributes = []string{"bad.*", "count.*", "otel.*", "value.*"}
	cfg.AdditionalInfo.MaxAttributes = 2
	cfg.AdditionalInfo.MaxValueLength = 3
	return cfg
}

func logsWithInvalidSeverity() plog.Logs {
	logs := sampleLogs()
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("servicenow.severity", "not-a-severity")
	return logs
}

func logsWithAllDiagnostics() plog.Logs {
	logs := sampleLogs()
	resourceAttrs := logs.ResourceLogs().At(0).Resource().Attributes()
	resourceAttrs.PutStr("servicenow.source", "long-source")
	resourceAttrs.PutStr("servicenow.event_class", "long-class")
	resourceAttrs.PutInt("servicenow.severity", 4)
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityText(strings.Repeat("S", 1200))
	attrs := logRecord.Attributes()
	attrs.PutStr("servicenow.severity", "invalid")
	attrs.PutStr("value.one", "abcdef")
	attrs.PutStr("value.two", "ghijkl")
	attrs.PutStr("count.one", "one")
	attrs.PutStr("count.two", "two")
	badMap := attrs.PutEmptyMap("bad.map")
	badMap.PutDouble("not_json", math.NaN())
	return logs
}

func startTelemetryTestExporter(
	t *testing.T,
	settings exporter.Settings,
	telemetry *componenttest.Telemetry,
	cfg *Config,
) (exporter.Logs, func()) {
	t.Helper()
	settings.TelemetrySettings = telemetry.NewTelemetrySettings()
	logsExporter, err := NewFactory().CreateLogs(context.Background(), settings, cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	shutdown := false
	shutdownExporter := func() {
		if shutdown {
			return
		}
		shutdown = true
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	}
	t.Cleanup(shutdownExporter)
	return logsExporter, shutdownExporter
}

func readMappingMeasurements(t *testing.T, telemetry *componenttest.Telemetry) []mappingDiagnosticMeasurement {
	t.Helper()
	metric, err := telemetry.GetMetric(mappingDiagnosticsMetricName)
	if err != nil {
		return nil
	}
	sum, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	return measurementsFromSum(t, sum)
}

func readCounterTotal(t *testing.T, telemetry *componenttest.Telemetry, name string) int64 {
	t.Helper()
	metric, err := telemetry.GetMetric(name)
	if err != nil {
		return 0
	}
	sum, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	var total int64
	for _, point := range sum.DataPoints {
		total += point.Value
	}
	return total
}

type mappingDiagnosticsFailureMeterProvider struct {
	metric.MeterProvider
}

func (mappingDiagnosticsFailureMeterProvider) Meter(name string, options ...metric.MeterOption) metric.Meter {
	return mappingDiagnosticsFailureMeter{Meter: noop.NewMeterProvider().Meter(name, options...)}
}

type mappingDiagnosticsFailureMeter struct {
	metric.Meter
}

func (m mappingDiagnosticsFailureMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if name == mappingDiagnosticsMetricName {
		return nil, errors.New("mapping counter construction failed")
	}
	return m.Meter.Int64Counter(name, options...)
}

func measurementsFromSum(t *testing.T, sum metricdata.Sum[int64]) []mappingDiagnosticMeasurement {
	t.Helper()
	measurements := make([]mappingDiagnosticMeasurement, 0, len(sum.DataPoints))
	for _, point := range sum.DataPoints {
		measurement := mappingDiagnosticMeasurement{value: point.Value}
		attributes := point.Attributes.ToSlice()
		for _, attr := range attributes {
			switch string(attr.Key) {
			case "reason":
				measurement.reason = attr.Value.AsString()
			case "exporter":
				measurement.exporter = attr.Value.AsString()
			default:
				t.Fatalf("unexpected mapping diagnostic attribute %q", attr.Key)
			}
		}
		require.Len(t, attributes, 2)
		measurements = append(measurements, measurement)
	}
	return measurements
}
