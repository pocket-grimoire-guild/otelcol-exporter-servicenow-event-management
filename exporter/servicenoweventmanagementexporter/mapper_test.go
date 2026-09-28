// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestMapLogsToRecords(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

	records, err := mapLogsToRecords(sampleLogs(), cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)

	record := records[0]
	require.Equal(t, "opentelemetry", record.Source)
	require.Equal(t, "checkout", record.EventClass)
	require.Equal(t, "host-01", record.Node)
	require.Equal(t, "checkout", record.Resource)
	require.Equal(t, "request.error_rate", record.MetricName)
	require.Equal(t, "application", record.Type)
	require.Equal(t, "checkout|host-01|request.error_rate", record.MessageKey)
	require.Equal(t, "2", record.Severity)
	require.Equal(t, "High error rate", record.Description)
	require.Equal(t, "2026-05-03 18:34:21", record.TimeOfEvent)
	require.Empty(t, record.ResolutionState)

	require.JSONEq(t, `{
		"otel.signal": "logs",
		"otel.severity_text": "ERROR",
		"otel.severity_number": "17",
		"deployment.environment": "prod",
		"service.name": "checkout",
		"host.name": "host-01",
		"event.name": "request.error_rate",
		"test.count": "7",
		"test.enabled": "true"
	}`, record.AdditionalInfo)
}
func TestMapLogsToRecordsDefaultCoverage(t *testing.T) {
	logs := plog.NewLogs()
	logRecord := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	logRecord.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)))
	logRecord.SetSeverityNumber(plog.SeverityNumberInfo)
	logRecord.Body().SetStr("Minimal event body")

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Len(t, records, 1)

	record := records[0]
	require.Equal(t, "opentelemetry", record.Source)
	require.Equal(t, "opentelemetry", record.EventClass)
	require.Empty(t, record.Node)
	require.Empty(t, record.Resource)
	require.Empty(t, record.MetricName)
	require.Equal(t, "otel-log", record.Type)
	require.Equal(t, "opentelemetry|otel-log", record.MessageKey)
	require.Equal(t, "5", record.Severity)
	require.Equal(t, "Minimal event body", record.Description)
	require.JSONEq(t, `{
		"otel.signal": "logs",
		"otel.severity_number": "9"
	}`, record.AdditionalInfo)
	require.Equal(t, "2026-05-04 12:00:00", record.TimeOfEvent)
	require.Empty(t, record.ResolutionState)
}

func TestMapLogsToRecordsUsesObservedTimestampFallback(t *testing.T) {
	logs := plog.NewLogs()
	logRecord := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	logRecord.SetObservedTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 4, 13, 14, 15, 0, time.UTC)))
	logRecord.Body().SetStr("Observed event body")

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "2026-05-04 13:14:15", records[0].TimeOfEvent)
}

func TestMapLogsToRecordsUsesCurrentTimeWhenNoTimestampsAreSet(t *testing.T) {
	logs := plog.NewLogs()
	logRecord := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	logRecord.Body().SetStr("Untimed event body")

	now := func() time.Time {
		return time.Date(2026, 5, 22, 18, 19, 20, 0, time.UTC)
	}
	records, err := mapLogsToRecordsWithClock(logs, createDefaultConfig().(*Config), now)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "2026-05-22 18:19:20", records[0].TimeOfEvent)
}

func TestMapLogsToRecordsFallsBackWhenConfiguredMessageKeyAttributesAreMissing(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Attributes = []string{"service.name", "missing.attribute", "event.name"}

	records, err := mapLogsToRecords(sampleLogs(), cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)

	record := records[0]
	require.Equal(t, "opentelemetry|host-01|application|checkout|request.error_rate", record.MessageKey)

	info := requireAdditionalInfo(t, record)
	require.Equal(t, "missing.attribute", info[messageKeyMissingAttributesKey])
	require.Equal(t, messageKeyStrategyFallbackDefaultFields, info[messageKeyStrategyKey])
}

func TestMapLogsToRecordsAppliesServiceNowFieldLimits(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Format = messageKeyFormatLegacy
	cfg.FieldLimits.EventClass = 5
	cfg.FieldLimits.MessageKey = 8
	cfg.FieldLimits.Description = 12

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.Attributes().PutStr("servicenow.source", strings.Repeat("s", serviceNowMaxSource+1))
	logRecord.Attributes().PutStr("servicenow.node", strings.Repeat("n", serviceNowMaxNode+1))
	logRecord.Attributes().PutStr("servicenow.event_class", "event-class-too-long")
	logRecord.Attributes().PutStr("servicenow.message_key", "message-key-too-long")
	logRecord.Attributes().PutStr("servicenow.description", "description-too-long")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)

	record := records[0]
	require.Equal(t, strings.Repeat("s", serviceNowMaxSource), record.Source)
	require.Equal(t, strings.Repeat("n", serviceNowMaxNode), record.Node)
	require.Equal(t, "event", record.EventClass)
	require.Equal(t, "message-", record.MessageKey)
	require.Equal(t, "description-", record.Description)

	info := requireAdditionalInfo(t, record)
	require.Contains(t, strings.Split(info[truncatedFieldsKey], ","), "source")
	require.Contains(t, strings.Split(info[truncatedFieldsKey], ","), "node")
	require.Contains(t, strings.Split(info[truncatedFieldsKey], ","), "event_class")
	require.Contains(t, strings.Split(info[truncatedFieldsKey], ","), "message_key")
	require.Contains(t, strings.Split(info[truncatedFieldsKey], ","), "description")
}

func TestMapLogsToRecordsHonorsConfiguredFieldLimitOverrides(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.Source = 251
	cfg.FieldLimits.Node = 150
	cfg.FieldLimits.MetricName = 1201

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	source := strings.Repeat("s", cfg.FieldLimits.Source)
	node := strings.Repeat("n", cfg.FieldLimits.Node)
	metricName := strings.Repeat("m", cfg.FieldLimits.MetricName)
	logRecord.Attributes().PutStr("servicenow.source", source)
	logRecord.Attributes().PutStr("servicenow.node", node)
	logRecord.Attributes().PutStr("servicenow.metric_name", metricName)
	logRecord.Attributes().PutStr("servicenow.message_key", "custom-key")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)

	record := records[0]
	require.Equal(t, source, record.Source)
	require.Equal(t, node, record.Node)
	require.Equal(t, metricName, record.MetricName)

	info := requireAdditionalInfo(t, record)
	require.NotContains(t, info, truncatedFieldsKey)
}

func TestMapLogsToRecordsUsesDefaultSeparatorWhenConfiguredSeparatorIsEmpty(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Separator = ""
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}

	records, err := mapLogsToRecords(sampleLogs(), cfg)
	require.NoError(t, err)
	require.Equal(t, "checkout|host-01|request.error_rate", records[0].MessageKey)
}

func TestMapLogsToRecordsTruncatesUTF8Safely(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.Description = 5

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.Attributes().PutStr("servicenow.description", "ok-☃-tail")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "ok-", records[0].Description)
	require.True(t, utf8.ValidString(records[0].Description))
}
