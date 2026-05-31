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
	return mapLogsToPayloadWithConfig(logs, newMappingConfig(cfg))
}

func mapLogsToPayloadWithConfig(logs plog.Logs, cfg mappingConfig) (eventPayload, error) {
	records, err := mapLogsToRecordsWithMapping(logs, cfg, time.Now)
	if err != nil {
		return eventPayload{}, err
	}
	return eventPayload{Records: records}, nil
}

func mapLogsToRecords(logs plog.Logs, cfg *Config) ([]eventRecord, error) {
	return mapLogsToRecordsWithMapping(logs, newMappingConfig(cfg), time.Now)
}

func mapLogsToRecordsWithClock(logs plog.Logs, cfg *Config, now func() time.Time) ([]eventRecord, error) {
	return mapLogsToRecordsWithMapping(logs, newMappingConfig(cfg), now)
}

func mapLogsToRecordsWithMapping(logs plog.Logs, cfg mappingConfig, now func() time.Time) ([]eventRecord, error) {
	records := make([]eventRecord, 0, logs.LogRecordCount())

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLogs := logs.ResourceLogs().At(i)
		resourceAttrs := resourceLogs.Resource().Attributes()

		for j := 0; j < resourceLogs.ScopeLogs().Len(); j++ {
			logRecords := resourceLogs.ScopeLogs().At(j).LogRecords()
			for k := 0; k < logRecords.Len(); k++ {
				logRecord := logRecords.At(k)
				record, err := mapLogRecordToEvent(logRecord, resourceAttrs, cfg, now)
				if err != nil {
					return nil, err
				}
				records = append(records, record)
			}
		}
	}

	return records, nil
}

func mapLogRecordToEvent(logRecord plog.LogRecord, resourceAttrs pcommon.Map, cfg mappingConfig, now func() time.Time) (eventRecord, error) {
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

	messageKeyValue := messageKey(logAttrs, resourceAttrs, cfg, source, node, eventType, resource, metricName)
	severity := serviceNowSeverity(logRecord, logAttrs, resourceAttrs, cfg)
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

	truncatedFields := applyFieldLimits(&record, cfg.FieldLimits)
	additionalInfo, err := additionalInfo(logRecord, logAttrs, resourceAttrs, cfg, messageKeyValue, truncatedFields)
	if err != nil {
		return eventRecord{}, err
	}
	record.AdditionalInfo = additionalInfo

	return record, nil
}
