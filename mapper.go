// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

type eventPayload struct {
	Records []eventRecord `json:"records"`
}

type eventRecord struct {
	Source          string `json:"source,omitempty"`
	EventClass      string `json:"event_class,omitempty"`
	Node            string `json:"node,omitempty"`
	Resource        string `json:"resource,omitempty"`
	MetricName      string `json:"metric_name,omitempty"`
	Type            string `json:"type,omitempty"`
	MessageKey      string `json:"message_key,omitempty"`
	Severity        string `json:"severity,omitempty"`
	Description     string `json:"description,omitempty"`
	AdditionalInfo  string `json:"additional_info,omitempty"`
	TimeOfEvent     string `json:"time_of_event,omitempty"`
	ResolutionState string `json:"resolution_state,omitempty"`
}

func mapLogsToPayload(logs plog.Logs, cfg *Config) (eventPayload, error) {
	payload, _, err := mapLogsToPayloadWithDiagnostics(logs, newMappingConfig(cfg))
	return payload, err
}

func mapLogsToPayloadWithDiagnostics(logs plog.Logs, cfg mappingConfig) (eventPayload, mappingDiagnosticSummary, error) {
	records, diagnostics, err := mapLogsToRecordsWithDiagnostics(logs, cfg, time.Now)
	if err != nil {
		return eventPayload{}, diagnostics, err
	}
	return eventPayload{Records: records}, diagnostics, nil
}

func mapLogsToRecords(logs plog.Logs, cfg *Config) ([]eventRecord, error) {
	return mapLogsToRecordsWithMapping(logs, newMappingConfig(cfg), time.Now)
}

func mapLogsToRecordsWithClock(logs plog.Logs, cfg *Config, now func() time.Time) ([]eventRecord, error) {
	return mapLogsToRecordsWithMapping(logs, newMappingConfig(cfg), now)
}

func mapLogsToRecordsWithMapping(logs plog.Logs, cfg mappingConfig, now func() time.Time) ([]eventRecord, error) {
	records, _, err := mapLogsToRecordsWithDiagnostics(logs, cfg, now)
	return records, err
}

func mapLogsToRecordsWithDiagnostics(logs plog.Logs, cfg mappingConfig, now func() time.Time) ([]eventRecord, mappingDiagnosticSummary, error) {
	records := make([]eventRecord, 0, logs.LogRecordCount())
	var diagnostics mappingDiagnosticSummary

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLogs := logs.ResourceLogs().At(i)
		resourceAttrs := resourceLogs.Resource().Attributes()

		for j := 0; j < resourceLogs.ScopeLogs().Len(); j++ {
			logRecords := resourceLogs.ScopeLogs().At(j).LogRecords()
			for k := 0; k < logRecords.Len(); k++ {
				logRecord := logRecords.At(k)
				record, flags, err := mapLogRecordToEventWithDiagnostics(logRecord, resourceAttrs, cfg, now)
				diagnostics.addRecord(flags)
				if err != nil {
					return nil, diagnostics, err
				}
				records = append(records, record)
			}
		}
	}

	return records, diagnostics, nil
}

func mapLogRecordToEventWithDiagnostics(
	logRecord plog.LogRecord,
	resourceAttrs pcommon.Map,
	cfg mappingConfig,
	now func() time.Time,
) (eventRecord, mappingDiagnosticFlags, error) {
	var diagnostics mappingDiagnosticFlags
	logAttrs := logRecord.Attributes()

	source := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.source"),
		cfg.Source,
		"opentelemetry",
	)
	eventClass := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.event_class"),
		cfg.EventClass,
		attrString(logAttrs, resourceAttrs, "service.name"),
		attrString(logAttrs, resourceAttrs, "event.domain"),
		source,
	)
	node := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.node"),
		attrString(logAttrs, resourceAttrs, "host.name"),
		attrString(logAttrs, resourceAttrs, "host.id"),
		attrString(logAttrs, resourceAttrs, "service.instance.id"),
		attrString(logAttrs, resourceAttrs, "net.host.name"),
	)
	resource := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.resource"),
		attrString(logAttrs, resourceAttrs, "service.name"),
		attrString(logAttrs, resourceAttrs, "service.namespace"),
	)
	metricName := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.metric_name"),
		attrString(logAttrs, resourceAttrs, "event.name"),
		logRecord.EventName(),
	)
	eventType := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.type"),
		cfg.EventType,
	)
	description := firstNonEmpty(
		attrString(logAttrs, resourceAttrs, "servicenow.description"),
		attrString(logAttrs, resourceAttrs, "event.description"),
		logRecord.Body().AsString(),
		logRecord.SeverityText(),
	)

	messageKeyValue, err := messageKey(logAttrs, resourceAttrs, cfg, source, node, eventType, resource, metricName)
	if err != nil {
		return eventRecord{}, diagnostics, err
	}
	if messageKeyValue.UsedConfiguredFallback {
		diagnostics = diagnostics.with(mappingDiagnosticConfiguredMessageKeyFallback)
	}
	severity, invalidSeverityOverride := serviceNowSeverityWithDiagnostic(logRecord, logAttrs, resourceAttrs, cfg)
	if invalidSeverityOverride {
		diagnostics = diagnostics.with(mappingDiagnosticInvalidSeverityOverride)
	}
	resolutionState := serviceNowResolutionState(logAttrs, resourceAttrs, severity, cfg)

	record := eventRecord{
		Source:          source,
		EventClass:      eventClass,
		Node:            node,
		Resource:        resource,
		MetricName:      metricName,
		Type:            eventType,
		MessageKey:      messageKeyValue.Value,
		Severity:        severity,
		Description:     description,
		TimeOfEvent:     serviceNowEventTime(logRecord, now),
		ResolutionState: resolutionState,
	}

	fieldLimits := cfg.FieldLimits
	if cfg.MessageKey.Format == messageKeyFormatSHA256V1 {
		// Explicit keys are checked against the budget during key selection, and generated digests are 79 bytes after config validation.
		fieldLimits.MessageKey = 0
	}
	truncatedFields := applyFieldLimits(&record, fieldLimits)
	if len(truncatedFields) > 0 {
		diagnostics = diagnostics.with(mappingDiagnosticEventFieldTruncated)
	}
	additionalInfoValue, additionalInfoDiagnostics, err := additionalInfoWithDiagnostics(logRecord, logAttrs, resourceAttrs, cfg, messageKeyValue, truncatedFields)
	diagnostics |= additionalInfoDiagnostics
	if err != nil {
		return eventRecord{}, diagnostics, err
	}
	record.AdditionalInfo = additionalInfoValue

	return record, diagnostics, nil
}
