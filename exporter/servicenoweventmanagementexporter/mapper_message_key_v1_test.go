// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestMessageKeySHA256V1KnownAnswers(t *testing.T) {
	t.Run("configured tuple with unicode and binary boundary content", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		cfg.MessageKey.Attributes = []string{"identity", "host"}

		logs := emptyIdentityLogs()
		attrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes()
		attrs.PutStr("identity", "é|x")
		attrs.PutStr("host", "node\x00blue")

		record := mapIdentityRecord(t, cfg, logs)
		require.Equal(t, "otel-sha256-v1:acb853b3c4b9a28e05ee55ce9291a343fea154a511bba4f1828d7a5761e5ce6f", record.MessageKey)
		require.Len(t, record.MessageKey, 79)
	})

	t.Run("default fields include empty positional slots", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		cfg.Source = "src"
		cfg.EventType = "CPU"

		record := mapIdentityRecord(t, cfg, emptyIdentityLogs())
		require.Equal(t, "otel-sha256-v1:3f046fda8012025c850e2b1d3e9ffb9ddf94b1f54f19c09a543a58cfe93e12f6", record.MessageKey)
		require.Len(t, record.MessageKey, 79)
	})
}

func TestMessageKeyMinimalRecordsRemainDeterministic(t *testing.T) {
	v1 := createDefaultConfig().(*Config)
	v1.MessageKey.Format = messageKeyFormatSHA256V1
	logs := logsWithMessageKeyRecords(map[string]string{}, map[string]string{})
	records, err := mapLogsToRecords(logs, v1)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, records[0].MessageKey, records[1].MessageKey)
	require.Equal(t, records[0].MessageKey, mapIdentityRecord(t, v1, emptyIdentityLogs()).MessageKey)

	emptyFormat := createDefaultConfig().(*Config)
	emptyFormat.MessageKey.Format = ""
	require.Equal(t, "opentelemetry|otel-log", mapIdentityRecord(t, emptyFormat, emptyIdentityLogs()).MessageKey)
}

func TestMessageKeySHA256V1TagsConfiguredAndDefaultBranches(t *testing.T) {
	pairs := []messageKeyPair{
		{Name: "source", Value: "src"},
		{Name: "node", Value: ""},
		{Name: "type", Value: "CPU"},
		{Name: "resource", Value: ""},
		{Name: "metric_name", Value: ""},
	}
	configured := hashMessageKeySHA256V1("attributes", pairs)
	defaultFields := hashMessageKeySHA256V1("default_fields", pairs)
	require.Equal(t, "otel-sha256-v1:ae666e21c831ee82a56262548b46abfbaf2a331c80d70db4055557963d206f89", configured)
	require.Equal(t, "otel-sha256-v1:3f046fda8012025c850e2b1d3e9ffb9ddf94b1f54f19c09a543a58cfe93e12f6", defaultFields)
	require.NotEqual(t, configured, defaultFields)
}

func TestMessageKeySHA256V1DistinguishesLegacyCollisionPairs(t *testing.T) {
	t.Run("configured separator values", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Attributes = []string{"part.a", "part.b"}
		left := logsWithLogAttributes(map[string]string{"part.a": "a|b", "part.b": "c"})
		right := logsWithLogAttributes(map[string]string{"part.a": "a", "part.b": "b|c"})

		legacyLeft := mapIdentityRecord(t, cfg, left)
		legacyRight := mapIdentityRecord(t, cfg, right)
		require.Equal(t, "a|b|c", legacyLeft.MessageKey)
		require.Equal(t, legacyLeft.MessageKey, legacyRight.MessageKey)

		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		v1Left := mapIdentityRecord(t, cfg, left)
		v1Right := mapIdentityRecord(t, cfg, right)
		require.NotEqual(t, v1Left.MessageKey, v1Right.MessageKey)
	})

	t.Run("default delimiter values", func(t *testing.T) {
		left := logsWithLogAttributes(map[string]string{
			"servicenow.source": "a|b",
			"servicenow.type":   "c",
		})
		right := logsWithLogAttributes(map[string]string{
			"servicenow.source": "a",
			"servicenow.type":   "b|c",
		})
		legacy := createDefaultConfig().(*Config)
		require.Equal(t, mapIdentityRecord(t, legacy, left).MessageKey, mapIdentityRecord(t, legacy, right).MessageKey)

		v1 := createDefaultConfig().(*Config)
		v1.MessageKey.Format = messageKeyFormatSHA256V1
		require.NotEqual(t, mapIdentityRecord(t, v1, left).MessageKey, mapIdentityRecord(t, v1, right).MessageKey)
	})

	t.Run("empty default field slots", func(t *testing.T) {
		left := logsWithLogAttributes(map[string]string{
			"servicenow.node":     "node",
			"servicenow.type":     "type",
			"servicenow.resource": "CPU",
		})
		right := logsWithLogAttributes(map[string]string{
			"servicenow.node":        "node",
			"servicenow.type":        "type",
			"servicenow.metric_name": "CPU",
		})
		legacy := createDefaultConfig().(*Config)
		require.Equal(t, mapIdentityRecord(t, legacy, left).MessageKey, mapIdentityRecord(t, legacy, right).MessageKey)

		v1 := createDefaultConfig().(*Config)
		v1.MessageKey.Format = messageKeyFormatSHA256V1
		require.NotEqual(t, mapIdentityRecord(t, v1, left).MessageKey, mapIdentityRecord(t, v1, right).MessageKey)
	})
}

func TestMessageKeySHA256V1HashesFullValuesBeforeFieldLimits(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.FieldLimits.Source = 100
	cfg.FieldLimits.MessageKey = 79

	common := strings.Repeat("s", 2200)
	left := logsWithLogAttributes(map[string]string{"servicenow.source": common + "left"})
	right := logsWithLogAttributes(map[string]string{"servicenow.source": common + "right"})

	leftRecord := mapIdentityRecord(t, cfg, left)
	rightRecord := mapIdentityRecord(t, cfg, right)
	require.Equal(t, strings.Repeat("s", 100), leftRecord.Source)
	require.Equal(t, leftRecord.Source, rightRecord.Source)
	require.NotEqual(t, leftRecord.MessageKey, rightRecord.MessageKey)
}

func TestMessageKeySHA256V1DistinguishesLegacyLongKeyCollision(t *testing.T) {
	fields := func(suffix string) plog.Logs {
		return logsWithLogAttributes(map[string]string{
			"servicenow.source":      strings.Repeat("s", serviceNowMaxSource),
			"servicenow.node":        strings.Repeat("n", serviceNowMaxNode),
			"servicenow.type":        strings.Repeat("t", serviceNowMaxType),
			"servicenow.resource":    strings.Repeat("r", serviceNowMaxResource),
			"servicenow.metric_name": strings.Repeat("m", 520) + suffix + strings.Repeat("m", serviceNowMaxMetricName-521),
		})
	}
	left := fields("a")
	right := fields("b")
	legacy := createDefaultConfig().(*Config)
	legacyLeft := mapIdentityRecord(t, legacy, left)
	legacyRight := mapIdentityRecord(t, legacy, right)
	require.Len(t, legacyLeft.MessageKey, serviceNowMaxMessageKey)
	require.Equal(t, legacyLeft.MessageKey, legacyRight.MessageKey)
	require.Equal(t, serviceNowMaxSource, len(legacyLeft.Source))
	require.Equal(t, serviceNowMaxNode, len(legacyLeft.Node))
	require.Equal(t, serviceNowMaxType, len(legacyLeft.Type))
	require.Equal(t, serviceNowMaxResource, len(legacyLeft.Resource))
	require.Equal(t, serviceNowMaxMetricName, len(legacyLeft.MetricName))

	v1 := createDefaultConfig().(*Config)
	v1.MessageKey.Format = messageKeyFormatSHA256V1
	v1Left := mapIdentityRecord(t, v1, left)
	v1Right := mapIdentityRecord(t, v1, right)
	require.NotEqual(t, v1Left.MessageKey, v1Right.MessageKey)
}

func TestMessageKeySHA256V1UsesConfiguredNamesOrderAndIgnoresSeparator(t *testing.T) {
	logs := logsWithLogAttributes(map[string]string{"first": "one", "second": "two"})
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.MessageKey.Attributes = []string{"first", "second"}
	cfg.MessageKey.Separator = "|"
	first := mapIdentityRecord(t, cfg, logs).MessageKey

	cfg.MessageKey.Separator = "::"
	require.Equal(t, first, mapIdentityRecord(t, cfg, logs).MessageKey)

	cfg.MessageKey.Attributes = []string{"second", "first"}
	reordered := mapIdentityRecord(t, cfg, logs).MessageKey
	require.NotEqual(t, first, reordered)

	cfg.MessageKey.Attributes = []string{"renamed.first", "second"}
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("renamed.first", "one")
	renamed := mapIdentityRecord(t, cfg, logs).MessageKey
	require.NotEqual(t, first, renamed)
}

func TestMessageKeySHA256V1FallbackAndExplicitAttributePrecedence(t *testing.T) {
	t.Run("log explicit key wins over resource key and configured tuple", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		cfg.MessageKey.Attributes = []string{"identity"}
		logs := emptyIdentityLogs()
		resourceAttrs := logs.ResourceLogs().At(0).Resource().Attributes()
		resourceAttrs.PutStr("servicenow.message_key", "resource-key")
		resourceAttrs.PutStr("identity", "resource-identity")
		logAttrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes()
		logAttrs.PutStr("servicenow.message_key", "log-key")
		logAttrs.PutStr("identity", "log-identity")

		require.Equal(t, "log-key", mapIdentityRecord(t, cfg, logs).MessageKey)
	})

	t.Run("log configured identity value wins over resource value", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		cfg.MessageKey.Attributes = []string{"identity"}
		logs := emptyIdentityLogs()
		logs.ResourceLogs().At(0).Resource().Attributes().PutStr("identity", "resource-identity")
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("identity", "log-identity")

		logValueWins := mapIdentityRecord(t, cfg, logs).MessageKey
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().Remove("identity")
		resourceValueWins := mapIdentityRecord(t, cfg, logs).MessageKey
		require.NotEqual(t, logValueWins, resourceValueWins)
	})

	t.Run("empty log explicit value falls back instead of using resource explicit key", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		logs := emptyIdentityLogs()
		logs.ResourceLogs().At(0).Resource().Attributes().PutStr("servicenow.message_key", "resource-key")
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("servicenow.message_key", "")

		record := mapIdentityRecord(t, cfg, logs)
		require.NotEqual(t, "resource-key", record.MessageKey)
		require.True(t, strings.HasPrefix(record.MessageKey, "otel-sha256-v1:"))
	})

	t.Run("empty log configured value shadows resource value and falls back", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		cfg.MessageKey.Attributes = []string{"identity"}
		logs := emptyIdentityLogs()
		logs.ResourceLogs().At(0).Resource().Attributes().PutStr("identity", "resource-identity")
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("identity", "")

		record := mapIdentityRecord(t, cfg, logs)
		defaultCfg := createDefaultConfig().(*Config)
		defaultCfg.MessageKey.Format = messageKeyFormatSHA256V1
		require.Equal(t, mapIdentityRecord(t, defaultCfg, logs).MessageKey, record.MessageKey)
		require.Equal(t, "identity", requireAdditionalInfo(t, record)[messageKeyMissingAttributesKey])
	})

	t.Run("resource explicit key uses established scalar string conversion", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		logs := emptyIdentityLogs()
		logs.ResourceLogs().At(0).Resource().Attributes().PutInt("servicenow.message_key", 42)
		require.Equal(t, "42", mapIdentityRecord(t, cfg, logs).MessageKey)
	})

	t.Run("missing configured values use the same default identity and preserve diagnostics", func(t *testing.T) {
		logs := logsWithLogAttributes(map[string]string{"configured": "present"})
		withoutConfiguredAttrs := createDefaultConfig().(*Config)
		withoutConfiguredAttrs.MessageKey.Format = messageKeyFormatSHA256V1
		configured := createDefaultConfig().(*Config)
		configured.MessageKey.Format = messageKeyFormatSHA256V1
		configured.MessageKey.Attributes = []string{"configured", "missing"}

		fallbackRecord := mapIdentityRecord(t, configured, logs)
		defaultRecord := mapIdentityRecord(t, withoutConfiguredAttrs, logs)
		require.Equal(t, defaultRecord.MessageKey, fallbackRecord.MessageKey)
		info := requireAdditionalInfo(t, fallbackRecord)
		require.Equal(t, "missing", info[messageKeyMissingAttributesKey])
		require.Equal(t, messageKeyStrategyFallbackDefaultFields, info[messageKeyStrategyKey])
	})
}

func TestMessageKeySHA256V1PreservesExplicitKeysWithinByteBudget(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		key   string
	}{
		{name: "exact ascii boundary", limit: 79, key: strings.Repeat("k", 79)},
		{name: "default byte boundary", limit: 1024, key: strings.Repeat("d", 1024)},
		{name: "exact unicode byte boundary", limit: 79, key: strings.Repeat("é", 39) + "a"},
		{name: "raised budget above default", limit: 1100, key: strings.Repeat("x", 1050)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.MessageKey.Format = messageKeyFormatSHA256V1
			cfg.MessageKey.Attributes = []string{"configured"}
			cfg.FieldLimits.MessageKey = tt.limit
			logs := logsWithLogAttributes(map[string]string{"configured": "tuple", "servicenow.message_key": tt.key})
			require.Equal(t, tt.key, mapIdentityRecord(t, cfg, logs).MessageKey)
		})
	}
}

func TestMessageKeySHA256V1RejectsOversizedExplicitKeysWithoutLeakingValues(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		key   string
	}{
		{name: "smallest valid budget", limit: 79, key: strings.Repeat("k", 80)},
		{name: "default budget", limit: 1024, key: strings.Repeat("p", 1025)},
		{name: "unicode byte overflow", limit: 79, key: strings.Repeat("é", 40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.MessageKey.Format = messageKeyFormatSHA256V1
			cfg.FieldLimits.MessageKey = tt.limit
			logs := logsWithLogAttributes(map[string]string{"servicenow.message_key": tt.key})
			logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().SetStr("private-event-body")

			_, err := mapLogsToRecords(logs, cfg)
			require.ErrorContains(t, err, "message_key")
			require.ErrorContains(t, err, "byte limit")
			require.NotContains(t, err.Error(), tt.key)
			require.NotContains(t, err.Error(), "private-event-body")
		})
	}
}

func TestMessageKeySHA256V1KeepsIdentityStableAcrossEventLifecycleFields(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.MessageKey.Attributes = []string{"service.name", "host.name", "event.name"}
	logs := sampleLogs()
	first := mapIdentityRecord(t, cfg, logs).MessageKey

	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityNumber(plog.SeverityNumberInfo)
	logRecord.Attributes().PutInt("servicenow.severity", 0)
	logRecord.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 9, 25, 1, 2, 3, 0, time.UTC)))
	logRecord.Body().SetStr("clear payload")
	second := mapIdentityRecord(t, cfg, logs).MessageKey
	require.Equal(t, first, second)
}

func emptyIdentityLogs() plog.Logs {
	logs := plog.NewLogs()
	logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	return logs
}

func logsWithLogAttributes(attrs map[string]string) plog.Logs {
	logs := emptyIdentityLogs()
	logAttrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes()
	for key, value := range attrs {
		logAttrs.PutStr(key, value)
	}
	return logs
}

func mapIdentityRecord(t *testing.T, cfg *Config, logs plog.Logs) eventRecord {
	t.Helper()
	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)
	return records[0]
}
