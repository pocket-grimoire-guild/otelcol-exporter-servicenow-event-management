// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

func serviceNowResolutionState(logAttrs, resourceAttrs pcommon.Map, severity string, cfg mappingConfig) string {
	resolutionState := normalizeServiceNowResolutionState(attrString(logAttrs, resourceAttrs, "servicenow.resolution_state"))
	if resolutionState == "" && severity == "0" {
		return normalizeServiceNowResolutionState(cfg.Severity.ClearResolutionState)
	}
	return resolutionState
}

func normalizeServiceNowResolutionState(value string) string {
	switch value = strings.TrimSpace(value); {
	case strings.EqualFold(value, serviceNowResolutionStateNew):
		return serviceNowResolutionStateNew
	case strings.EqualFold(value, serviceNowResolutionStateClosing):
		return serviceNowResolutionStateClosing
	default:
		return ""
	}
}
