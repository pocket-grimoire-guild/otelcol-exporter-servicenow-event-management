// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"encoding/json"
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
)

func TestFactoryMessageKeySHA256V1TransmitsCollisionAndLifecycleKeys(t *testing.T) {
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
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.MessageKey.Attributes = []string{"part.a", "part.b"}
	exporter := startFactoryMessageKeyExporter(t, cfg)

	logs := logsWithMessageKeyRecords(
		map[string]string{"part.a": "a|b", "part.b": "c"},
		map[string]string{"part.a": "a", "part.b": "b|c"},
		map[string]string{"part.a": "stable", "part.b": "node", "servicenow.severity": "2"},
		map[string]string{"part.a": "stable", "part.b": "node", "servicenow.severity": "0"},
		map[string]string{"part.a": "explicit-tuple", "part.b": "node", "servicenow.message_key": "explicit-key"},
	)
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(2).Body().SetStr("firing body")
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(3).Body().SetStr("clear body")
	logs.MarkReadOnly()
	require.NoError(t, exporter.ConsumeLogs(context.Background(), logs))

	select {
	case payload := <-received:
		require.Len(t, payload.Records, 5)
		require.NotEqual(t, payload.Records[0].MessageKey, payload.Records[1].MessageKey)
		require.Equal(t, payload.Records[2].MessageKey, payload.Records[3].MessageKey)
		require.Equal(t, "2", payload.Records[2].Severity)
		require.Equal(t, "0", payload.Records[3].Severity)
		require.Equal(t, serviceNowResolutionStateClosing, payload.Records[3].ResolutionState)
		require.Equal(t, "explicit-key", payload.Records[4].MessageKey)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for factory exporter request")
	}
}

func TestFactoryMessageKeySHA256V1TransmitsFallbackCollisionAndFullValueKeys(t *testing.T) {
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
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.EventType = ""
	cfg.FieldLimits.Source = 100
	cfg.FieldLimits.MessageKey = 79
	exporter := startFactoryMessageKeyExporter(t, cfg)

	common := strings.Repeat("s", 2200)
	longTuple := func(suffix string) map[string]string {
		return map[string]string{
			"servicenow.source":      strings.Repeat("s", cfg.FieldLimits.Source),
			"servicenow.node":        strings.Repeat("n", cfg.FieldLimits.Node),
			"servicenow.type":        strings.Repeat("t", cfg.FieldLimits.Type),
			"servicenow.resource":    strings.Repeat("r", cfg.FieldLimits.Resource),
			"servicenow.metric_name": strings.Repeat("m", 620) + suffix + strings.Repeat("m", cfg.FieldLimits.MetricName-621),
		}
	}
	logs := logsWithMessageKeyRecords(
		map[string]string{"servicenow.source": "a|b", "servicenow.type": "c"},
		map[string]string{"servicenow.source": "a", "servicenow.type": "b|c"},
		map[string]string{"servicenow.node": "node", "servicenow.type": "type", "servicenow.resource": "CPU"},
		map[string]string{"servicenow.node": "node", "servicenow.type": "type", "servicenow.metric_name": "CPU"},
		map[string]string{"servicenow.source": common + "left"},
		map[string]string{"servicenow.source": common + "right"},
		longTuple("a"),
		longTuple("b"),
	)
	logs.MarkReadOnly()
	require.NoError(t, exporter.ConsumeLogs(context.Background(), logs))

	select {
	case payload := <-received:
		require.Len(t, payload.Records, 8)
		require.NotEqual(t, payload.Records[0].MessageKey, payload.Records[1].MessageKey)
		require.NotEqual(t, payload.Records[2].MessageKey, payload.Records[3].MessageKey)
		require.Equal(t, strings.Repeat("s", 100), payload.Records[4].Source)
		require.Equal(t, payload.Records[4].Source, payload.Records[5].Source)
		require.NotEqual(t, payload.Records[4].MessageKey, payload.Records[5].MessageKey)
		require.Equal(t, cfg.FieldLimits.MetricName, len(payload.Records[6].MetricName))
		require.Equal(t, cfg.FieldLimits.MetricName, len(payload.Records[7].MetricName))
		require.NotEqual(t, payload.Records[6].MessageKey, payload.Records[7].MessageKey)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for factory exporter request")
	}
}

func TestFactoryMessageKeySHA256V1RejectsMixedBatchPermanentlyBeforeTransport(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
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
	cfg.BackOffConfig.MaxElapsedTime = time.Second
	cfg.BackOffConfig.RandomizationFactor = 0
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.FieldLimits.MessageKey = 79
	exporter := startFactoryMessageKeyExporter(t, cfg)

	oversized := strings.Repeat("private-key-", 8)
	logs := logsWithMessageKeyRecords(
		map[string]string{"servicenow.message_key": "valid-key"},
		map[string]string{"servicenow.message_key": oversized},
	)
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().SetStr("valid-body")
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(1).Body().SetStr("private-body")
	logs.MarkReadOnly()
	err := exporter.ConsumeLogs(context.Background(), logs)
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err), "mapping overflow must be marked permanent")
	require.NotContains(t, err.Error(), oversized)
	require.NotContains(t, err.Error(), "private-body")
	require.Equal(t, int32(0), requests.Load())

	logRecords := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	key, ok := logRecords.At(0).Attributes().Get("servicenow.message_key")
	require.True(t, ok)
	require.Equal(t, "valid-key", key.Str())
	key, ok = logRecords.At(1).Attributes().Get("servicenow.message_key")
	require.True(t, ok)
	require.Equal(t, oversized, key.Str())
	require.Equal(t, "private-body", logRecords.At(1).Body().Str())
}

func startFactoryMessageKeyExporter(t *testing.T, cfg *Config) exporter.Logs {
	t.Helper()
	logsExporter, err := NewFactory().CreateLogs(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	t.Cleanup(func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	})
	return logsExporter
}

func logsWithMessageKeyRecords(records ...map[string]string) plog.Logs {
	logs := plog.NewLogs()
	logRecords := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	for _, attrs := range records {
		logRecord := logRecords.AppendEmpty()
		for key, value := range attrs {
			logRecord.Attributes().PutStr(key, value)
		}
	}
	return logs
}
