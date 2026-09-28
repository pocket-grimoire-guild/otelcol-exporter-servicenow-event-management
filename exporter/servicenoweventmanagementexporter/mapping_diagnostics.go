// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

type mappingDiagnosticReason uint8

const (
	mappingDiagnosticInvalidSeverityOverride mappingDiagnosticReason = iota
	mappingDiagnosticConfiguredMessageKeyFallback
	mappingDiagnosticEventFieldTruncated
	mappingDiagnosticAdditionalInfoValueTruncated
	mappingDiagnosticAdditionalInfoAttributeDropped
	mappingDiagnosticAdditionalInfoMetadataReduced
	mappingDiagnosticReasonCount
)

var mappingDiagnosticReasonNames = [...]string{
	"invalid_severity_override",
	"configured_message_key_fallback",
	"event_field_truncated",
	"additional_info_value_truncated",
	"additional_info_attribute_dropped",
	"additional_info_metadata_reduced",
}

type mappingDiagnosticFlags uint8

func (flags mappingDiagnosticFlags) with(reason mappingDiagnosticReason) mappingDiagnosticFlags {
	return flags | 1<<reason
}

func (flags mappingDiagnosticFlags) has(reason mappingDiagnosticReason) bool {
	return flags&(1<<reason) != 0
}

type mappingDiagnosticSummary struct {
	counts [mappingDiagnosticReasonCount]int64
}

func (summary *mappingDiagnosticSummary) addRecord(flags mappingDiagnosticFlags) {
	for reason := mappingDiagnosticReason(0); reason < mappingDiagnosticReasonCount; reason++ {
		if flags.has(reason) {
			summary.counts[reason]++
		}
	}
}
