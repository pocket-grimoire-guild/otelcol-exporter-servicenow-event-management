// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestMapLogsToRecordsSeverityMapping(t *testing.T) {
	tests := []struct {
		name     string
		severity plog.SeverityNumber
		want     string
	}{
		{name: "fatal", severity: plog.SeverityNumberFatal, want: "1"},
		{name: "error", severity: plog.SeverityNumberError, want: "2"},
		{name: "warn", severity: plog.SeverityNumberWarn, want: "4"},
		{name: "info", severity: plog.SeverityNumberInfo, want: "5"},
		{name: "unspecified", severity: plog.SeverityNumberUnspecified, want: "5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.SetSeverityNumber(tt.severity)
			record.Attributes().Remove("servicenow.severity")

			records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
			require.NoError(t, err)
			require.Equal(t, tt.want, records[0].Severity)
		})
	}
}

func TestMapLogsToRecordsSupportsCustomSeverityMapping(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Severity.Mapping.Error = 5

	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().Remove("servicenow.severity")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Equal(t, "5", records[0].Severity)
}

func TestMapLogsToRecordsFallsBackWhenExplicitSeverityIsInvalid(t *testing.T) {
	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().PutStr("servicenow.severity", "critical")

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Equal(t, "2", records[0].Severity)
}

func TestMapLogsToRecordsUsesConfiguredSeverityAttribute(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Severity.FromAttribute = "custom.severity"

	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().Remove("servicenow.severity")
	record.Attributes().PutStr("custom.severity", " 4 ")

	records, err := mapLogsToRecords(logs, cfg)
	require.NoError(t, err)
	require.Equal(t, "4", records[0].Severity)
}

func TestMapLogsToRecordsFallsBackForUnsupportedExplicitSeverityTypes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(plog.LogRecord)
	}{
		{
			name: "out of range number",
			mutate: func(record plog.LogRecord) {
				record.Attributes().PutInt("servicenow.severity", 9)
			},
		},
		{
			name: "unsupported boolean",
			mutate: func(record plog.LogRecord) {
				record.Attributes().PutBool("servicenow.severity", true)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.SetSeverityNumber(plog.SeverityNumberError)
			tt.mutate(record)

			records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
			require.NoError(t, err)
			require.Equal(t, "2", records[0].Severity)
		})
	}
}

func TestMapLogsToRecordsFallsBackForFractionalDoubleSeverity(t *testing.T) {
	for _, value := range []float64{-0.5, 0.9, 4.9, 5.9, math.Nextafter(4, 5)} {
		t.Run(strconv.FormatFloat(value, 'g', -1, 64), func(t *testing.T) {
			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.Attributes().PutDouble("servicenow.severity", value)

			records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
			require.NoError(t, err)
			require.Equal(t, "2", records[0].Severity)
			require.Empty(t, records[0].ResolutionState)
		})
	}
}

func TestMapLogsToRecordsFallsBackForNonFiniteAndLargeDoubleSeverity(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value float64
	}{
		{name: "nan", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
		{name: "large finite", value: math.MaxFloat64},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.Attributes().PutDouble("servicenow.severity", tt.value)

			records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
			require.NoError(t, err)
			require.Equal(t, "2", records[0].Severity)
			require.Empty(t, records[0].ResolutionState)
		})
	}
}

func TestMapLogsToRecordsAcceptsIntegralDoubleSeverity(t *testing.T) {
	for value := float64(0); value <= 5; value++ {
		t.Run(strconv.FormatFloat(value, 'g', -1, 64), func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.Severity.Mapping.Error = (int(value) + 1) % 6

			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.Attributes().PutDouble("servicenow.severity", value)

			records, err := mapLogsToRecords(logs, cfg)
			require.NoError(t, err)
			require.Equal(t, strconv.Itoa(int(value)), records[0].Severity)
			if value == 0 {
				require.Equal(t, "Closing", records[0].ResolutionState)
			} else {
				require.Empty(t, records[0].ResolutionState)
			}
		})
	}
}

func TestMapLogsToRecordsInvalidDoubleSeverityKeepsExistingFallbackPolicies(t *testing.T) {
	t.Run("configured mapping to clear remains intentional", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.Severity.Mapping.Error = 0

		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutDouble("servicenow.severity", 4.9)

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, "0", records[0].Severity)
		require.Equal(t, "Closing", records[0].ResolutionState)
	})

	t.Run("configured default to clear remains intentional", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.Severity.Default = 0

		logs := sampleLogs()
		record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
		record.SetSeverityNumber(plog.SeverityNumberUnspecified)
		record.Attributes().PutDouble("servicenow.severity", 4.9)

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, "0", records[0].Severity)
		require.Equal(t, "Closing", records[0].ResolutionState)
	})

	t.Run("configured clear state can be omitted", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.Severity.Mapping.Error = 0
		cfg.Severity.ClearResolutionState = ""

		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutDouble("servicenow.severity", 0.9)

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, "0", records[0].Severity)
		require.Empty(t, records[0].ResolutionState)
	})

	t.Run("configured clear state is preserved", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.Severity.Mapping.Error = 0
		cfg.Severity.ClearResolutionState = "New"

		logs := sampleLogs()
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutDouble("servicenow.severity", 4.9)

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, "0", records[0].Severity)
		require.Equal(t, "New", records[0].ResolutionState)
	})
}

func TestMapLogsToRecordsPreservesExplicitResolutionStateWhenDoubleSeverityFallsBack(t *testing.T) {
	for _, resolutionState := range []string{"New", "Closing"} {
		t.Run(resolutionState, func(t *testing.T) {
			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.Attributes().PutDouble("servicenow.severity", 4.9)
			record.Attributes().PutStr("servicenow.resolution_state", resolutionState)

			records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
			require.NoError(t, err)
			require.Equal(t, "2", records[0].Severity)
			require.Equal(t, resolutionState, records[0].ResolutionState)
		})
	}
}

func TestMapLogsToRecordsSeverityOverridePrecedenceAndCustomAttribute(t *testing.T) {
	t.Run("resource override uses the same validation", func(t *testing.T) {
		logs := sampleLogs()
		resource := logs.ResourceLogs().At(0).Resource().Attributes()
		resource.PutDouble("servicenow.severity", 4.9)
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().Remove("servicenow.severity")

		records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
		require.NoError(t, err)
		require.Equal(t, "2", records[0].Severity)
		require.Empty(t, records[0].ResolutionState)
	})

	t.Run("invalid log override shadows valid resource override", func(t *testing.T) {
		logs := sampleLogs()
		resource := logs.ResourceLogs().At(0).Resource().Attributes()
		resource.PutInt("servicenow.severity", 4)
		logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutDouble("servicenow.severity", 4.9)

		records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
		require.NoError(t, err)
		require.Equal(t, "2", records[0].Severity)
		require.Empty(t, records[0].ResolutionState)
	})

	t.Run("configured custom attribute uses the same validation", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.Severity.FromAttribute = "custom.severity"

		logs := sampleLogs()
		record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
		record.Attributes().Remove("servicenow.severity")
		record.Attributes().PutDouble("custom.severity", 0.9)

		records, err := mapLogsToRecords(logs, cfg)
		require.NoError(t, err)
		require.Equal(t, "2", records[0].Severity)
		require.Empty(t, records[0].ResolutionState)
	})

	t.Run("invalid override keeps unspecified severity mapping", func(t *testing.T) {
		logs := sampleLogs()
		record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
		record.SetSeverityNumber(plog.SeverityNumberUnspecified)
		record.Attributes().PutDouble("servicenow.severity", 0.9)

		records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
		require.NoError(t, err)
		require.Equal(t, "5", records[0].Severity)
		require.Empty(t, records[0].ResolutionState)
	})
}

func TestMapLogsToRecordsSetsClosingResolutionStateForClearSeverity(t *testing.T) {
	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().PutInt("servicenow.severity", 0)

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Equal(t, "0", records[0].Severity)
	require.Equal(t, "Closing", records[0].ResolutionState)
}

func TestMapLogsToRecordsKeepsExplicitResolutionStateForClearSeverity(t *testing.T) {
	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().PutInt("servicenow.severity", 0)
	record.Attributes().PutStr("servicenow.resolution_state", "New")

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Equal(t, "0", records[0].Severity)
	require.Equal(t, "New", records[0].ResolutionState)
}

func TestMapLogsToRecordsNormalizesExplicitResolutionState(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "new", value: " new ", want: "New"},
		{name: "closing", value: " closing ", want: "Closing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logs := sampleLogs()
			record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
			record.Attributes().PutStr("servicenow.resolution_state", tt.value)

			records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
			require.NoError(t, err)
			require.Equal(t, tt.want, records[0].ResolutionState)
		})
	}
}

func TestMapLogsToRecordsDropsInvalidExplicitResolutionState(t *testing.T) {
	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().PutStr("servicenow.resolution_state", "Closed")

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Empty(t, records[0].ResolutionState)
}

func TestMapLogsToRecordsFallsBackToClearResolutionStateWhenExplicitResolutionStateInvalid(t *testing.T) {
	logs := sampleLogs()
	record := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	record.Attributes().PutInt("servicenow.severity", 0)
	record.Attributes().PutStr("servicenow.resolution_state", "Closed")

	records, err := mapLogsToRecords(logs, createDefaultConfig().(*Config))
	require.NoError(t, err)
	require.Equal(t, "0", records[0].Severity)
	require.Equal(t, "Closing", records[0].ResolutionState)
}
