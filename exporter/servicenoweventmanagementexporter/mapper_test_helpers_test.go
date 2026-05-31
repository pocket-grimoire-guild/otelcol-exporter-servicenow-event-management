// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func sampleLogs() plog.Logs {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("service.name", "checkout")
	resourceLogs.Resource().Attributes().PutStr("host.name", "host-01")
	resourceLogs.Resource().Attributes().PutStr("deployment.environment", "prod")

	logRecord := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	logRecord.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 3, 18, 34, 21, 0, time.UTC)))
	logRecord.SetSeverityNumber(plog.SeverityNumberError)
	logRecord.SetSeverityText("ERROR")
	logRecord.Body().SetStr("High error rate")
	logRecord.Attributes().PutStr("event.name", "request.error_rate")
	logRecord.Attributes().PutInt("test.count", 7)
	logRecord.Attributes().PutBool("test.enabled", true)
	logRecord.Attributes().PutInt("servicenow.severity", 2)
	logRecord.Attributes().PutStr("servicenow.type", "application")

	return logs
}

func requireAdditionalInfo(t *testing.T, record eventRecord) map[string]string {
	t.Helper()

	info := map[string]string{}
	require.NoError(t, json.Unmarshal([]byte(record.AdditionalInfo), &info))
	return info
}
