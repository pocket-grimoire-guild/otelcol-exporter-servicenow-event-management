// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/collector/pdata/plog"
)

const serviceNowTimeLayout = "2006-01-02 15:04:05"

func serviceNowEventTime(logRecord plog.LogRecord, now func() time.Time) string {
	timestamp := logRecord.Timestamp()
	if timestamp == 0 {
		timestamp = logRecord.ObservedTimestamp()
	}
	if timestamp == 0 {
		return now().UTC().Format(serviceNowTimeLayout)
	}
	return timestamp.AsTime().UTC().Format(serviceNowTimeLayout)
}

func applyFieldLimits(record *eventRecord, limits FieldLimitsConfig) []string {
	truncated := make([]string, 0)
	limitField := func(name string, value *string, maxBytes int) {
		limited := truncateString(*value, maxBytes)
		if limited != *value {
			*value = limited
			truncated = append(truncated, name)
		}
	}

	limitField("source", &record.Source, limits.Source)
	limitField("event_class", &record.EventClass, limits.EventClass)
	limitField("node", &record.Node, limits.Node)
	limitField("resource", &record.Resource, limits.Resource)
	limitField("metric_name", &record.MetricName, limits.MetricName)
	limitField("type", &record.Type, limits.Type)
	limitField("message_key", &record.MessageKey, limits.MessageKey)
	limitField("description", &record.Description, limits.Description)
	limitField("resolution_state", &record.ResolutionState, limits.ResolutionState)

	return truncated
}

func truncateString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}

	truncated := value[:maxBytes]
	for !utf8.ValidString(truncated) {
		_, size := utf8.DecodeLastRuneInString(truncated)
		if size <= 0 || size > len(truncated) {
			return ""
		}
		truncated = truncated[:len(truncated)-size]
	}
	return truncated
}
