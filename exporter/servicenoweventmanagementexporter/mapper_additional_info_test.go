// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
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

func TestMapLogsToRecordsReturnsErrorWhenRequiredAdditionalInfoExceedsLimit(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.FieldLimits.AdditionalInfo = 256

	logs := sampleLogs()
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	logRecord.SetSeverityText(strings.Repeat("x", 512))

	_, err := mapLogsToPayload(logs, cfg)
	require.ErrorContains(t, err, "required metadata exceeds")
}
