// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

const (
	messageKeyMissingAttributesKey          = "otel.servicenow.message_key.missing_attributes"
	messageKeyStrategyKey                   = "otel.servicenow.message_key.strategy"
	messageKeyStrategyFallbackDefaultFields = "fallback_default_fields"
	messageKeySHA256V1Domain                = "servicenow_event_management.message_key.sha256_v1"
	messageKeySHA256V1Prefix                = "otel-sha256-v1:"
)

var defaultMessageKeyPartNames = [...]string{"source", "node", "type", "resource", "metric_name"}

type messageKeyPair struct {
	Name  string
	Value string
}

type messageKeyResult struct {
	Value                       string
	MissingConfiguredAttributes []string
	UsedConfiguredFallback      bool
}

func messageKey(logAttrs, resourceAttrs pcommon.Map, cfg mappingConfig, parts ...string) (messageKeyResult, error) {
	if key := attrString(logAttrs, resourceAttrs, "servicenow.message_key"); key != "" {
		if cfg.MessageKey.Format == messageKeyFormatSHA256V1 && len(key) > cfg.FieldLimits.MessageKey {
			return messageKeyResult{}, fmt.Errorf("explicit message_key exceeds configured byte limit (%d)", cfg.FieldLimits.MessageKey)
		}
		return messageKeyResult{Value: key}, nil
	}

	if len(cfg.MessageKey.Attributes) > 0 {
		pairs := make([]messageKeyPair, 0, len(cfg.MessageKey.Attributes))
		missing := make([]string, 0, len(cfg.MessageKey.Attributes))
		for _, attr := range cfg.MessageKey.Attributes {
			if value := attrString(logAttrs, resourceAttrs, attr); value != "" {
				pairs = append(pairs, messageKeyPair{Name: attr, Value: value})
			} else {
				missing = append(missing, attr)
			}
		}
		if len(missing) == 0 {
			if cfg.MessageKey.Format == messageKeyFormatSHA256V1 {
				return messageKeyResult{Value: hashMessageKeySHA256V1("attributes", pairs)}, nil
			}
			values := make([]string, len(pairs))
			for index, pair := range pairs {
				values[index] = pair.Value
			}
			return messageKeyResult{Value: strings.Join(values, messageKeySeparator(cfg))}, nil
		}

		if cfg.MessageKey.Format == messageKeyFormatSHA256V1 {
			return messageKeyResult{
				Value:                       hashMessageKeySHA256V1("default_fields", defaultMessageKeyPairs(parts)),
				MissingConfiguredAttributes: missing,
				UsedConfiguredFallback:      true,
			}, nil
		}

		return messageKeyResult{
			Value:                       defaultMessageKey(cfg, parts...),
			MissingConfiguredAttributes: missing,
			UsedConfiguredFallback:      true,
		}, nil
	}

	if cfg.MessageKey.Format == messageKeyFormatSHA256V1 {
		return messageKeyResult{Value: hashMessageKeySHA256V1("default_fields", defaultMessageKeyPairs(parts))}, nil
	}
	return messageKeyResult{Value: defaultMessageKey(cfg, parts...)}, nil
}

func defaultMessageKeyPairs(parts []string) []messageKeyPair {
	pairs := make([]messageKeyPair, len(defaultMessageKeyPartNames))
	for index, name := range defaultMessageKeyPartNames {
		value := ""
		if index < len(parts) {
			value = parts[index]
		}
		pairs[index] = messageKeyPair{Name: name, Value: value}
	}
	return pairs
}

func hashMessageKeySHA256V1(branch string, pairs []messageKeyPair) string {
	digest := sha256.New()
	writeMessageKeyFrame(digest, messageKeySHA256V1Domain)
	writeMessageKeyFrame(digest, branch)
	writeMessageKeyUint64(digest, uint64(len(pairs)))
	for _, pair := range pairs {
		writeMessageKeyFrame(digest, pair.Name)
		writeMessageKeyFrame(digest, pair.Value)
	}
	return messageKeySHA256V1Prefix + hex.EncodeToString(digest.Sum(nil))
}

func writeMessageKeyFrame(digest hash.Hash, value string) {
	writeMessageKeyUint64(digest, uint64(len(value)))
	_, _ = digest.Write([]byte(value))
}

func writeMessageKeyUint64(digest hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = digest.Write(encoded[:])
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
