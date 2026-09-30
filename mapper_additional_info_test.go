// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestMapLogsToRecordsControlsAdditionalInfoAttributes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.IncludeAttributes = []string{"safe.*", "password", "authorization.*", "large.*"}
	cfg.AdditionalInfo.MaxValueLength = 5

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.Attributes().PutStr("safe.keep", "yes")
	logRecord.Attributes().PutStr("password", "super-secret")
	logRecord.Attributes().PutStr("authorization.header", "Bearer secret")
	logRecord.Attributes().PutStr("large.value", "abcdefghij")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)

	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "yes", info["safe.keep"])
	require.Equal(t, redactedValue, info["password"])
	require.Equal(t, redactedValue, info["authorization.header"])
	require.Equal(t, "abcde", info["large.value"])
	require.Equal(t, "2", info[additionalInfoRedactedAttributesKey])
	require.Equal(t, "1", info[additionalInfoTruncatedValuesKey])
	require.NotContains(t, info, "deployment.environment")
}

func TestMapLogsToRecordsExcludesAdditionalInfoAttributes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.ExcludeAttributes = []string{"drop.*", "service.name"}

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.Attributes().PutStr("drop.secret", "hidden")
	logRecord.Attributes().PutStr("keep.value", "visible")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)

	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "visible", info["keep.value"])
	require.NotContains(t, info, "drop.secret")
	require.NotContains(t, info, "service.name")
}

func TestMapLogsToRecordsKeepsCustomAlertFieldAdditionalInfoKeys(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.IncludeAttributes = []string{"u_*", "user_*", "x_acme_*"}

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.Attributes().PutStr("u_service_owner", "payments")
	logRecord.Attributes().PutStr("user_impact_tier", "gold")
	logRecord.Attributes().PutStr("x_acme_alert_ticket", "INC0010001")
	logRecord.Attributes().PutStr("deployment.environment", "prod")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)

	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "payments", info["u_service_owner"])
	require.Equal(t, "gold", info["user_impact_tier"])
	require.Equal(t, "INC0010001", info["x_acme_alert_ticket"])
	require.NotContains(t, info, "deployment.environment")
}

func TestMapLogsToRecordsRendersComplexAdditionalInfoAttributes(t *testing.T) {
	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	arrayAttr := logRecord.Attributes().PutEmptySlice("complex.array")
	arrayAttr.AppendEmpty().SetStr("alpha")
	arrayAttr.AppendEmpty().SetInt(7)
	mapAttr := logRecord.Attributes().PutEmptyMap("complex.map")
	mapAttr.PutStr("name", "checkout")
	mapAttr.PutInt("count", 2)
	bytesAttr := logRecord.Attributes().PutEmptyBytes("complex.bytes")
	bytesAttr.Append(0x01, 0x02, 0x03)

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)

	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, `["alpha",7]`, info["complex.array"])
	require.Equal(t, `{"count":2,"name":"checkout"}`, info["complex.map"])
	require.Equal(t, "AQID", info["complex.bytes"])
}

func TestRenderAdditionalInfoValueUsesDocumentedRepresentations(t *testing.T) {
	tests := []struct {
		name  string
		value pcommon.Value
		want  string
	}{
		{
			name:  "string",
			value: pcommon.NewValueStr("alpha"),
			want:  "alpha",
		},
		{
			name:  "int",
			value: pcommon.NewValueInt(7),
			want:  "7",
		},
		{
			name:  "double",
			value: pcommon.NewValueDouble(12.25),
			want:  "12.25",
		},
		{
			name:  "bool",
			value: pcommon.NewValueBool(true),
			want:  "true",
		},
		{
			name: "map",
			value: func() pcommon.Value {
				value := pcommon.NewValueMap()
				value.Map().PutStr("name", "checkout")
				value.Map().PutInt("count", 2)
				return value
			}(),
			want: `{"count":2,"name":"checkout"}`,
		},
		{
			name: "slice",
			value: func() pcommon.Value {
				value := pcommon.NewValueSlice()
				value.Slice().AppendEmpty().SetStr("alpha")
				value.Slice().AppendEmpty().SetInt(7)
				return value
			}(),
			want: `["alpha",7]`,
		},
		{
			name: "bytes",
			value: func() pcommon.Value {
				value := pcommon.NewValueBytes()
				value.Bytes().Append(0x01, 0x02, 0x03)
				return value
			}(),
			want: "AQID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, ok := renderAdditionalInfoValue(tt.value)
			require.True(t, ok)
			require.Equal(t, tt.want, rendered)
		})
	}
}

func TestMapLogsToRecordsCountsAdditionalInfoValuesThatCannotRender(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.IncludeAttributes = []string{"bad.*"}

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	badMap := logRecord.Attributes().PutEmptyMap("bad.map")
	badMap.PutDouble("not_json", math.NaN())

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)

	info := requireAdditionalInfo(t, records[0])
	require.NotContains(t, info, "bad.map")
	require.Equal(t, "1", info[additionalInfoDroppedByValueKey])
}

func TestMapLogsToRecordsKeepsAdditionalInfoWithinConfiguredLimit(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256
	cfg.AdditionalInfo.MaxAttributes = 0

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	for i := 0; i < 20; i++ {
		logRecord.Attributes().PutStr("bulk."+strconv.Itoa(i), strings.Repeat("x", 80))
	}

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)

	require.LessOrEqual(t, len(records[0].AdditionalInfo), cfg.FieldLimits.AdditionalInfo)
	info := requireAdditionalInfo(t, records[0])
	require.NotEmpty(t, info[additionalInfoDroppedAttributesKey])
}

func TestMapLogsToRecordsDefaultAdditionalInfoValueLimitUsesFieldBudget(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.IncludeAttributes = []string{"large.value"}

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	value := strings.Repeat("x", 1500)
	logRecord.Attributes().PutStr("large.value", value)

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.LessOrEqual(t, len(records[0].AdditionalInfo), cfg.FieldLimits.AdditionalInfo)

	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, value, info["large.value"])
	require.NotContains(t, info, additionalInfoTruncatedValuesKey)
}

func TestMapLogsToRecordsLimitsAdditionalInfoAttributeCount(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.MaxAttributes = 2

	records, err := mapLogsToRecords(sampleLogs(), cfg)
	require.NoError(t, err)
	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "4", info[additionalInfoDroppedByCountKey])
	require.Contains(t, info, "deployment.environment")
	require.Contains(t, info, "event.name")
	require.NotContains(t, info, "host.name")
}

func TestMapLogsToRecordsKeepsOverflowMetadataBoundedAtMinimumLimit(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityText(strings.Repeat("x", 4096))

	payload, err := mapLogsToPayload(logs, cfg)
	require.NoError(t, err)
	records := payload.Records
	require.Len(t, records, 1)
	require.LessOrEqual(t, len(records[0].AdditionalInfo), cfg.FieldLimits.AdditionalInfo)

	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "logs", info["otel.signal"])
	require.Equal(t, "17", info["otel.severity_number"])
	require.Equal(t, "true", info["otel.servicenow.additional_info.metadata_reduced"])
	require.NotContains(t, info, "otel.severity_text")
}

func TestMapLogsToRecordsOverflowFitsEncodedMetadataByPriority(t *testing.T) {
	const (
		metadataReducedKey = "otel.servicenow.additional_info.metadata_reduced"
		droppedAttributes  = "otel.servicenow.additional_info.dropped_attributes"
	)

	severityText := strings.Repeat("λ\"\\\n\u0001", 12)
	longMissing := []string{
		"missing." + strings.Repeat("a", 180),
		"missing." + strings.Repeat("b", 170),
	}
	makeLogs := func() plog.Logs {
		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetSeverityText(severityText)
		return logs
	}
	makeConfig := func(limit int) *Config {
		cfg := createDefaultConfig().(*Config)
		cfg.FieldLimits.AdditionalInfo = limit
		cfg.AdditionalInfo.MaxAttributes = 0
		cfg.MessageKey.Attributes = append([]string(nil), longMissing...)
		return cfg
	}

	optional, _ := additionalInfoAttributeValues(
		makeLogs().ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes(),
		makeLogs().ResourceLogs().At(0).Resource().Attributes(),
		makeConfig(256).AdditionalInfo,
	)
	base := map[string]string{
		"otel.signal":          "logs",
		"otel.severity_number": "17",
		metadataReducedKey:     "true",
		droppedAttributes:      strconv.Itoa(len(optional)),
	}
	encodedBase, err := json.Marshal(base)
	require.NoError(t, err)
	fit := make(map[string]string, len(base)+1)
	for key, value := range base {
		fit[key] = value
	}
	fit["otel.severity_text"] = severityText
	encodedFit, err := json.Marshal(fit)
	require.NoError(t, err)
	exactFitLimit := len(encodedFit)

	for _, tt := range []struct {
		name         string
		limit        int
		wantSeverity bool
		wantStrategy bool
		wantMissing  bool
		severity     string
	}{
		{name: "exact encoded fit", limit: exactFitLimit, wantSeverity: true, severity: severityText},
		{name: "one byte over", limit: exactFitLimit - 1, wantSeverity: false, wantStrategy: true, severity: severityText},
		{name: "minimum with escaped controls", limit: 256, wantSeverity: false, wantStrategy: true, severity: severityText},
		{name: "nearby budget", limit: 257, wantSeverity: false, wantStrategy: true, severity: severityText},
		{name: "default field budget", limit: 4000, wantSeverity: false, wantStrategy: true, wantMissing: true, severity: strings.Repeat("界", 5000)},
		{name: "raised field budget", limit: 12000, wantSeverity: false, wantStrategy: true, wantMissing: true, severity: strings.Repeat("界", 5000)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeConfig(tt.limit)
			logs := makeLogs()
			logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetSeverityText(tt.severity)

			records, err := mapLogsToRecords(logs, cfg)
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "opentelemetry|host-01|application|checkout|request.error_rate", records[0].MessageKey)
			require.Equal(t, "2", records[0].Severity)
			require.LessOrEqual(t, len(records[0].AdditionalInfo), tt.limit)

			info := requireAdditionalInfo(t, records[0])
			require.Equal(t, "true", info[metadataReducedKey])
			require.Equal(t, "logs", info["otel.signal"])
			require.Equal(t, "17", info["otel.severity_number"])
			if tt.wantSeverity {
				require.Equal(t, tt.severity, info["otel.severity_text"])
				require.True(t, utf8.ValidString(info["otel.severity_text"]))
			} else {
				require.NotContains(t, info, "otel.severity_text")
			}
			require.Equal(t, strconv.Itoa(len(optional)), info["otel.servicenow.additional_info.dropped_attributes"])
			if tt.wantStrategy {
				require.Equal(t, messageKeyStrategyFallbackDefaultFields, info[messageKeyStrategyKey])
			} else {
				require.NotContains(t, info, messageKeyStrategyKey)
			}
			if tt.wantMissing {
				require.Equal(t, strings.Join(longMissing, ","), info[messageKeyMissingAttributesKey])
			} else {
				require.NotContains(t, info, messageKeyMissingAttributesKey)
			}

			repeated, err := mapLogsToRecords(logs, cfg)
			require.NoError(t, err)
			require.Equal(t, records[0].AdditionalInfo, repeated[0].AdditionalInfo)
		})
	}
	require.LessOrEqual(t, len(encodedBase), 256)
}

func TestAdditionalInfoMaximumReducedEnvelopeFitsMinimumBudget(t *testing.T) {
	info := map[string]string{
		"otel.signal":          "logs",
		"otel.severity_number": strconv.FormatInt(-1<<31, 10),
		"otel.servicenow.additional_info.metadata_reduced":   "true",
		"otel.servicenow.additional_info.dropped_attributes": strconv.FormatInt(1<<63-1, 10),
	}
	encoded, err := json.Marshal(info)
	require.NoError(t, err)
	require.Equal(t, 192, len(encoded))
	require.LessOrEqual(t, len(encoded), 256)
}

func TestMapLogsToRecordsKeepsExactSuccessfulAdditionalInfoOutputs(t *testing.T) {
	t.Run("ordinary metadata", func(t *testing.T) {
		records, err := mapLogsToRecords(sampleLogs(), createDefaultConfig().(*Config))
		require.NoError(t, err)
		require.Equal(t, `{"deployment.environment":"prod","event.name":"request.error_rate","host.name":"host-01","otel.severity_number":"17","otel.severity_text":"ERROR","otel.signal":"logs","service.name":"checkout","test.count":"7","test.enabled":"true"}`, records[0].AdditionalInfo)
	})

	t.Run("ordinary optional shedding", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.FieldLimits.AdditionalInfo = 256
		cfg.AdditionalInfo.IncludeAttributes = []string{"safe.*"}
		logs := sampleLogs()
		attrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes()
		attrs.PutStr("safe.a", "one")
		attrs.PutStr("safe.b", "two")
		attrs.PutStr("safe.z", strings.Repeat("x", 180))

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, `{"otel.servicenow.additional_info.dropped_attributes":"1","otel.severity_number":"17","otel.severity_text":"ERROR","otel.signal":"logs","safe.a":"one","safe.b":"two"}`, records[0].AdditionalInfo)
	})

	t.Run("redaction and custom alert field", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.AdditionalInfo.IncludeAttributes = []string{"password", "u_service_owner"}
		logs := sampleLogs()
		logs.ResourceLogs().At(0).Resource().Attributes().PutStr("u_service_owner", "payments")
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr("password", "secret-value")

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, `{"otel.servicenow.additional_info.redacted_attributes":"1","otel.severity_number":"17","otel.severity_text":"ERROR","otel.signal":"logs","password":"[REDACTED]","u_service_owner":"payments"}`, records[0].AdditionalInfo)
	})
}

func TestMapLogsToRecordsComposedDiagnosticsDoNotRejectOverflow(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256
	cfg.FieldLimits.Source = 3
	cfg.AdditionalInfo.IncludeAttributes = []string{"bad.*", "count.*", "long.*", "redact.*"}
	cfg.AdditionalInfo.RedactAttributes = []string{"redact.*"}
	cfg.AdditionalInfo.MaxAttributes = 2
	cfg.AdditionalInfo.MaxValueLength = 4
	cfg.MessageKey.Attributes = []string{"missing." + strings.Repeat("x", 180), "missing." + strings.Repeat("y", 180)}

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityText(strings.Repeat("z", 1024))
	logRecord.Attributes().PutStr("long.value", "abcdefgh")
	logRecord.Attributes().PutStr("redact.secret", "secret")
	logRecord.Attributes().PutStr("count.one", "1")
	logRecord.Attributes().PutStr("count.two", "2")
	badMap := logRecord.Attributes().PutEmptyMap("bad.map")
	badMap.PutDouble("not_json", math.NaN())

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "ope", records[0].Source)
	require.LessOrEqual(t, len(records[0].AdditionalInfo), 256)

	_, stats := additionalInfoAttributeValues(logRecord.Attributes(), logs.ResourceLogs().At(0).Resource().Attributes(), cfg.AdditionalInfo)
	require.Equal(t, 2, stats.droppedByCount)
	require.Equal(t, 1, stats.droppedByValue)
	require.Equal(t, 1, stats.redacted)
	require.Equal(t, 1, stats.truncatedValues)
	expected, err := json.Marshal(map[string]string{
		"otel.signal":          "logs",
		"otel.severity_number": "17",
		"otel.servicenow.additional_info.metadata_reduced":   "true",
		"otel.servicenow.additional_info.dropped_attributes": "2",
		messageKeyStrategyKey:                                messageKeyStrategyFallbackDefaultFields,
	})
	require.NoError(t, err)
	require.Equal(t, string(expected), records[0].AdditionalInfo)
}

func TestMapLogsToRecordsOverflowRetainsCountersInPriorityOrder(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.AdditionalInfo.IncludeAttributes = []string{"bad.*", "count.*", "long.*", "redact.*"}
	cfg.AdditionalInfo.RedactAttributes = []string{"redact.*"}
	cfg.AdditionalInfo.MaxAttributes = 2
	cfg.AdditionalInfo.MaxValueLength = 4

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityNumber(plog.SeverityNumberUnspecified)
	logRecord.SetSeverityText(strings.Repeat("z", 1024))
	logRecord.Attributes().PutStr("long.value", "abcdefgh")
	logRecord.Attributes().PutStr("redact.secret", "secret")
	logRecord.Attributes().PutStr("count.one", "1")
	logRecord.Attributes().PutStr("count.two", "2")
	badMap := logRecord.Attributes().PutEmptyMap("bad.map")
	badMap.PutDouble("not_json", math.NaN())

	expected, err := json.Marshal(map[string]string{
		"otel.signal": "logs",
		"otel.servicenow.additional_info.metadata_reduced":    "true",
		"otel.servicenow.additional_info.dropped_attributes":  "2",
		"otel.servicenow.additional_info.dropped_by_count":    "2",
		"otel.servicenow.additional_info.redacted_attributes": "1",
	})
	require.NoError(t, err)
	require.LessOrEqual(t, len(expected), 256)
	cfg.FieldLimits.AdditionalInfo = 256

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, string(expected), records[0].AdditionalInfo)
	require.NotContains(t, records[0].AdditionalInfo, "dropped_by_value")
	require.NotContains(t, records[0].AdditionalInfo, "truncated_values")
}

func TestMapLogsToRecordsMinimumErrorFallbackKeepsExactPriorityMetadata(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256
	cfg.MessageKey.Attributes = []string{"service.name", "deployment.environment"}

	logs := sampleLogs()
	resource := logs.ResourceLogs().At(0).Resource().Attributes()
	resource.Remove("deployment.environment")
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.Attributes().PutStr("optional.value", strings.Repeat("x", 40))

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "opentelemetry|host-01|application|checkout|request.error_rate", records[0].MessageKey)
	require.Equal(t, "2", records[0].Severity)
	require.LessOrEqual(t, len(records[0].AdditionalInfo), 256)
	require.Equal(t, `{"otel.servicenow.additional_info.dropped_attributes":"6","otel.servicenow.additional_info.metadata_reduced":"true","otel.severity_number":"17","otel.severity_text":"ERROR","otel.signal":"logs"}`, records[0].AdditionalInfo)
}

func TestMapLogsToRecordsOverflowContinuesAfterUnfitMetadata(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256
	cfg.FieldLimits.Source = 3
	cfg.AdditionalInfo.IncludeAttributes = []string{"never.*"}
	cfg.MessageKey.Attributes = []string{"missing." + strings.Repeat("x", 200), "missing." + strings.Repeat("y", 200)}

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityNumber(plog.SeverityNumberUnspecified)
	logRecord.SetSeverityText("")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "true", info["otel.servicenow.additional_info.metadata_reduced"])
	require.Equal(t, messageKeyStrategyFallbackDefaultFields, info[messageKeyStrategyKey])
	require.NotContains(t, info, messageKeyMissingAttributesKey)
	require.Equal(t, "source", info["otel.servicenow.truncated_fields"])
}

func TestMapLogsToRecordsReservesReducedMarkerFromProducerAttributes(t *testing.T) {
	const markerKey = "otel.servicenow.additional_info.metadata_reduced"

	for _, source := range []string{"resource", "log", "both"} {
		for _, overflow := range []bool{false, true} {
			t.Run(source+map[bool]string{false: "/ordinary", true: "/overflow"}[overflow], func(t *testing.T) {
				cfg := createDefaultConfig().(*Config)
				if overflow {
					cfg.FieldLimits.AdditionalInfo = 256
				}
				logs := sampleLogs()
				logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
				if overflow {
					logRecord.SetSeverityText(strings.Repeat("x", 1024))
				}
				if source == "resource" || source == "both" {
					logs.ResourceLogs().At(0).Resource().Attributes().PutStr(markerKey, "resource-forgery")
				}
				if source == "log" || source == "both" {
					logRecord.Attributes().PutStr(markerKey, "log-forgery")
				}

				records, err := mapLogsToRecords(logs, cfg)
				require.NoError(t, err)
				info := requireAdditionalInfo(t, records[0])
				if overflow {
					require.Equal(t, "true", info[markerKey])
					require.Equal(t, "7", info["otel.servicenow.additional_info.dropped_attributes"])
				} else {
					require.NotContains(t, info, markerKey)
					require.Equal(t, "1", info["otel.servicenow.additional_info.dropped_attributes"])
				}
			})
		}
	}

	t.Run("filtered marker is not copied or counted", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.AdditionalInfo.ExcludeAttributes = []string{markerKey}
		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr(markerKey, "filtered-forgery")

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		info := requireAdditionalInfo(t, records[0])
		require.NotContains(t, info, markerKey)
		require.NotContains(t, info, "otel.servicenow.additional_info.dropped_attributes")
	})

	t.Run("included marker follows redaction before being reserved", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.AdditionalInfo.IncludeAttributes = []string{markerKey}
		cfg.AdditionalInfo.RedactAttributes = []string{markerKey}
		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutStr(markerKey, "producer-forgery")

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		info := requireAdditionalInfo(t, records[0])
		require.NotContains(t, info, markerKey)
		require.Equal(t, "1", info["otel.servicenow.additional_info.dropped_attributes"])
		require.Equal(t, "1", info["otel.servicenow.additional_info.redacted_attributes"])
	})
}

func TestMapLogsToRecordsOverflowUsesComputedDroppedAttributeCount(t *testing.T) {
	const droppedKey = "otel.servicenow.additional_info.dropped_attributes"
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityText(strings.Repeat("x", 1024))
	logRecord.Attributes().PutStr("otel.signal", "producer-signal")
	logRecord.Attributes().PutStr(droppedKey, "producer-count")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	info := requireAdditionalInfo(t, records[0])
	require.Equal(t, "8", info[droppedKey])
	require.NotContains(t, info, "producer-count")
	require.Equal(t, "logs", info["otel.signal"])
}
