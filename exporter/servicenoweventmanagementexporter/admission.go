// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

type admissionLogsExporter struct {
	exporter.Logs
	now func() time.Time
}

func newAdmissionLogsExporter(next exporter.Logs, now func() time.Time) exporter.Logs {
	return &admissionLogsExporter{Logs: next, now: now}
}

func (e *admissionLogsExporter) ConsumeLogs(ctx context.Context, logs plog.Logs) error {
	return e.Logs.ConsumeLogs(ctx, prepareAdmissionLogs(logs, e.now))
}

// prepareAdmissionLogs fills missing observed timestamps before helper queueing and retry can repeat mapping.
// Recovered queue requests with no timestamps bypass this wrapper and use the mapper's delivery-time fallback.
func prepareAdmissionLogs(logs plog.Logs, now func() time.Time) plog.Logs {
	needsObservedTimestamp := false
	for resourceIndex := 0; resourceIndex < logs.ResourceLogs().Len() && !needsObservedTimestamp; resourceIndex++ {
		resourceLogs := logs.ResourceLogs().At(resourceIndex)
		for scopeIndex := 0; scopeIndex < resourceLogs.ScopeLogs().Len() && !needsObservedTimestamp; scopeIndex++ {
			logRecords := resourceLogs.ScopeLogs().At(scopeIndex).LogRecords()
			for recordIndex := 0; recordIndex < logRecords.Len(); recordIndex++ {
				logRecord := logRecords.At(recordIndex)
				if logRecord.Timestamp() == 0 && logRecord.ObservedTimestamp() == 0 {
					needsObservedTimestamp = true
					break
				}
			}
		}
	}
	if !needsObservedTimestamp {
		return logs
	}

	observedTimestamp := pcommon.NewTimestampFromTime(now())
	prepared := plog.NewLogs()
	logs.CopyTo(prepared)
	for resourceIndex := 0; resourceIndex < prepared.ResourceLogs().Len(); resourceIndex++ {
		resourceLogs := prepared.ResourceLogs().At(resourceIndex)
		for scopeIndex := 0; scopeIndex < resourceLogs.ScopeLogs().Len(); scopeIndex++ {
			logRecords := resourceLogs.ScopeLogs().At(scopeIndex).LogRecords()
			for recordIndex := 0; recordIndex < logRecords.Len(); recordIndex++ {
				logRecord := logRecords.At(recordIndex)
				if logRecord.Timestamp() == 0 && logRecord.ObservedTimestamp() == 0 {
					logRecord.SetObservedTimestamp(observedTimestamp)
				}
			}
		}
	}
	return prepared
}
