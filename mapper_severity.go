// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"math"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func serviceNowSeverityWithDiagnostic(logRecord plog.LogRecord, logAttrs, resourceAttrs pcommon.Map, cfg mappingConfig) (string, bool) {
	attrName := firstNonEmpty(cfg.Severity.FromAttribute, "servicenow.severity")
	if value, ok := attrValue(logAttrs, resourceAttrs, attrName); ok {
		if severity, ok := parseServiceNowSeverity(value); ok {
			return strconv.Itoa(severity), false
		}
		return mappedServiceNowSeverity(logRecord.SeverityNumber(), cfg), true
	}
	return mappedServiceNowSeverity(logRecord.SeverityNumber(), cfg), false
}

func mappedServiceNowSeverity(severity plog.SeverityNumber, cfg mappingConfig) string {
	switch {
	case severity >= plog.SeverityNumberFatal:
		return strconv.Itoa(cfg.Severity.Mapping.Fatal)
	case severity >= plog.SeverityNumberError:
		return strconv.Itoa(cfg.Severity.Mapping.Error)
	case severity >= plog.SeverityNumberWarn:
		return strconv.Itoa(cfg.Severity.Mapping.Warn)
	case severity >= plog.SeverityNumberInfo:
		return strconv.Itoa(cfg.Severity.Mapping.Info)
	default:
		return strconv.Itoa(cfg.Severity.Default)
	}
}

func parseServiceNowSeverity(value pcommon.Value) (int, bool) {
	var severity int
	switch value.Type() {
	case pcommon.ValueTypeInt:
		severity = int(value.Int())
	case pcommon.ValueTypeDouble:
		double := value.Double()
		if math.IsNaN(double) || math.IsInf(double, 0) || double < 0 || double > 5 || math.Trunc(double) != double {
			return 0, false
		}
		severity = int(double)
	case pcommon.ValueTypeStr:
		parsed, err := strconv.Atoi(strings.TrimSpace(value.Str()))
		if err != nil {
			return 0, false
		}
		severity = parsed
	default:
		return 0, false
	}

	if severity < 0 || severity > 5 {
		return 0, false
	}
	return severity, true
}
