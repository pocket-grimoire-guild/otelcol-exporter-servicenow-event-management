// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

const (
	messageKeyMissingAttributesKey          = "otel.servicenow.message_key.missing_attributes"
	messageKeyStrategyKey                   = "otel.servicenow.message_key.strategy"
	messageKeyStrategyFallbackDefaultFields = "fallback_default_fields"
)

type messageKeyResult struct {
	Value                       string
	MissingConfiguredAttributes []string
	UsedConfiguredFallback      bool
}

func messageKey(logAttrs, resourceAttrs pcommon.Map, cfg mappingConfig, parts ...string) messageKeyResult {
	if key := attrString(logAttrs, resourceAttrs, "servicenow.message_key"); key != "" {
		return messageKeyResult{Value: key}
	}

	if len(cfg.MessageKey.Attributes) > 0 {
		values := make([]string, 0, len(cfg.MessageKey.Attributes))
		missing := make([]string, 0, len(cfg.MessageKey.Attributes))
		for _, attr := range cfg.MessageKey.Attributes {
			if value := attrString(logAttrs, resourceAttrs, attr); value != "" {
				values = append(values, value)
			} else {
				missing = append(missing, attr)
			}
		}
		if len(missing) == 0 {
			return messageKeyResult{Value: strings.Join(values, messageKeySeparator(cfg))}
		}

		return messageKeyResult{
			Value:                       defaultMessageKey(cfg, parts...),
			MissingConfiguredAttributes: missing,
			UsedConfiguredFallback:      true,
		}
	}

	return messageKeyResult{Value: defaultMessageKey(cfg, parts...)}
}

func defaultMessageKey(cfg mappingConfig, parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, messageKeySeparator(cfg))
}

func messageKeySeparator(cfg mappingConfig) string {
	if cfg.MessageKey.Separator != "" {
		return cfg.MessageKey.Separator
	}
	return defaultMessageKeySeparator
}

func attrString(logAttrs, resourceAttrs pcommon.Map, key string) string {
	value, ok := attrValue(logAttrs, resourceAttrs, key)
	if !ok {
		return ""
	}
	return value.AsString()
}

func attrValue(logAttrs, resourceAttrs pcommon.Map, key string) (pcommon.Value, bool) {
	if value, ok := logAttrs.Get(key); ok {
		return value, true
	}
	return resourceAttrs.Get(key)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
