// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

const (
	additionalInfoDroppedAttributesKey  = "otel.servicenow.additional_info.dropped_attributes"
	additionalInfoDroppedByCountKey     = "otel.servicenow.additional_info.dropped_by_count"
	additionalInfoDroppedByValueKey     = "otel.servicenow.additional_info.dropped_by_value"
	additionalInfoRedactedAttributesKey = "otel.servicenow.additional_info.redacted_attributes"
	additionalInfoTruncatedValuesKey    = "otel.servicenow.additional_info.truncated_values"
	truncatedFieldsKey                  = "otel.servicenow.truncated_fields"
	redactedValue                       = "[REDACTED]"
)

type additionalInfoEntry struct {
	key   string
	value string
}

type additionalInfoStats struct {
	droppedByCount  int
	droppedByValue  int
	redacted        int
	truncatedValues int
}

func additionalInfo(
	logRecord plog.LogRecord,
	logAttrs, resourceAttrs pcommon.Map,
	cfg mappingConfig,
	messageKey messageKeyResult,
	truncatedFields []string,
) (string, error) {
	required := []additionalInfoEntry{{key: "otel.signal", value: "logs"}}
	if logRecord.SeverityText() != "" {
		required = append(required, additionalInfoEntry{key: "otel.severity_text", value: logRecord.SeverityText()})
	}
	if logRecord.SeverityNumber() != plog.SeverityNumberUnspecified {
		required = append(required, additionalInfoEntry{
			key:   "otel.severity_number",
			value: strconv.FormatInt(int64(logRecord.SeverityNumber()), 10),
		})
	}
	if len(messageKey.MissingConfiguredAttributes) > 0 {
		required = append(required, additionalInfoEntry{
			key:   messageKeyMissingAttributesKey,
			value: strings.Join(messageKey.MissingConfiguredAttributes, ","),
		})
	}
	if messageKey.UsedConfiguredFallback {
		required = append(required, additionalInfoEntry{
			key:   messageKeyStrategyKey,
			value: messageKeyStrategyFallbackDefaultFields,
		})
	}
	if len(truncatedFields) > 0 {
		required = append(required, additionalInfoEntry{
			key:   truncatedFieldsKey,
			value: strings.Join(truncatedFields, ","),
		})
	}

	attrValues, stats := additionalInfoAttributeValues(logAttrs, resourceAttrs, cfg.AdditionalInfo)
	if stats.droppedByCount > 0 {
		required = append(required, additionalInfoEntry{
			key:   additionalInfoDroppedByCountKey,
			value: strconv.Itoa(stats.droppedByCount),
		})
	}
	if stats.redacted > 0 {
		required = append(required, additionalInfoEntry{
			key:   additionalInfoRedactedAttributesKey,
			value: strconv.Itoa(stats.redacted),
		})
	}
	if stats.droppedByValue > 0 {
		required = append(required, additionalInfoEntry{
			key:   additionalInfoDroppedByValueKey,
			value: strconv.Itoa(stats.droppedByValue),
		})
	}
	if stats.truncatedValues > 0 {
		required = append(required, additionalInfoEntry{
			key:   additionalInfoTruncatedValuesKey,
			value: strconv.Itoa(stats.truncatedValues),
		})
	}

	keys := make([]string, 0, len(attrValues))
	for key := range attrValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	optional := make([]additionalInfoEntry, 0, len(keys))
	for _, key := range keys {
		optional = append(optional, additionalInfoEntry{key: key, value: attrValues[key]})
	}

	return fitAdditionalInfo(required, optional, cfg.FieldLimits.AdditionalInfo)
}

func additionalInfoAttributeValues(logAttrs, resourceAttrs pcommon.Map, cfg AdditionalInfoConfig) (map[string]string, additionalInfoStats) {
	values := make(map[string]string, resourceAttrs.Len()+logAttrs.Len())
	stats := additionalInfoStats{}
	resourceAttrs.Range(func(key string, value pcommon.Value) bool {
		collectAdditionalInfoAttr(values, &stats, key, value, cfg)
		return true
	})
	logAttrs.Range(func(key string, value pcommon.Value) bool {
		collectAdditionalInfoAttr(values, &stats, key, value, cfg)
		return true
	})

	if cfg.MaxAttributes > 0 && len(values) > cfg.MaxAttributes {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys[cfg.MaxAttributes:] {
			delete(values, key)
			stats.droppedByCount++
		}
	}

	return values, stats
}

func collectAdditionalInfoAttr(values map[string]string, stats *additionalInfoStats, key string, value pcommon.Value, cfg AdditionalInfoConfig) {
	if !shouldIncludeAdditionalInfoAttribute(key, cfg) {
		return
	}

	rendered, ok := renderAdditionalInfoValue(value)
	if !ok {
		stats.droppedByValue++
		return
	}
	if matchesAnyPattern(key, cfg.RedactAttributes) {
		rendered = redactedValue
		stats.redacted++
	} else if truncated := truncateString(rendered, cfg.MaxValueLength); truncated != rendered {
		rendered = truncated
		stats.truncatedValues++
	}
	values[key] = rendered
}

func renderAdditionalInfoValue(value pcommon.Value) (string, bool) {
	switch value.Type() {
	case pcommon.ValueTypeEmpty:
		return "", true
	case pcommon.ValueTypeStr:
		return value.Str(), true
	case pcommon.ValueTypeInt:
		return strconv.FormatInt(value.Int(), 10), true
	case pcommon.ValueTypeDouble:
		return strconv.FormatFloat(value.Double(), 'g', -1, 64), true
	case pcommon.ValueTypeBool:
		return strconv.FormatBool(value.Bool()), true
	case pcommon.ValueTypeMap:
		return renderAdditionalInfoJSON(value.Map().AsRaw())
	case pcommon.ValueTypeSlice:
		return renderAdditionalInfoJSON(value.Slice().AsRaw())
	case pcommon.ValueTypeBytes:
		return base64.StdEncoding.EncodeToString(value.Bytes().AsRaw()), true
	default:
		return "", false
	}
}

func renderAdditionalInfoJSON(value any) (string, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func shouldIncludeAdditionalInfoAttribute(key string, cfg AdditionalInfoConfig) bool {
	if strings.HasPrefix(key, "servicenow.") {
		return false
	}
	if len(cfg.IncludeAttributes) > 0 && !matchesAnyPattern(key, cfg.IncludeAttributes) {
		return false
	}
	if matchesAnyPattern(key, cfg.ExcludeAttributes) {
		return false
	}
	return true
}

func fitAdditionalInfo(required, optional []additionalInfoEntry, maxBytes int) (string, error) {
	info := make(map[string]string, len(required)+len(optional))
	for _, entry := range required {
		info[entry.key] = entry.value
	}

	includedOptional := make([]string, 0, len(optional))
	dropped := 0
	for _, entry := range optional {
		if _, exists := info[entry.key]; exists {
			dropped++
			continue
		}
		info[entry.key] = entry.value
		encoded, err := json.Marshal(info)
		if err != nil {
			return "", fmt.Errorf("marshal ServiceNow additional_info: %w", err)
		}
		if len(encoded) <= maxBytes {
			includedOptional = append(includedOptional, entry.key)
			continue
		}
		delete(info, entry.key)
		dropped++
	}

	if dropped > 0 {
		info[additionalInfoDroppedAttributesKey] = strconv.Itoa(dropped)
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("marshal ServiceNow additional_info: %w", err)
	}
	for len(encoded) > maxBytes && len(includedOptional) > 0 {
		last := includedOptional[len(includedOptional)-1]
		includedOptional = includedOptional[:len(includedOptional)-1]
		delete(info, last)
		dropped++
		info[additionalInfoDroppedAttributesKey] = strconv.Itoa(dropped)

		encoded, err = json.Marshal(info)
		if err != nil {
			return "", fmt.Errorf("marshal ServiceNow additional_info: %w", err)
		}
	}

	if len(encoded) > maxBytes {
		return "", fmt.Errorf("ServiceNow additional_info required metadata exceeds configured limit of %d bytes", maxBytes)
	}
	return string(encoded), nil
}

func matchesAnyPattern(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesPattern(value, pattern) {
			return true
		}
	}
	return false
}

func matchesPattern(value, pattern string) bool {
	value = strings.ToLower(value)
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	matched, err := path.Match(pattern, value)
	if err == nil {
		return matched
	}
	return pattern == value
}
