// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
)

func TestMessageKeyConfigMapstructureFormat(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	conf := confmap.NewFromStringMap(map[string]any{
		"message_key": map[string]any{"format": "sha256_v1"},
	})
	require.NoError(t, conf.Unmarshal(cfg))
	require.Equal(t, messageKeyFormatSHA256V1, cfg.MessageKey.Format)
}

func TestMessageKeyFormatDefaultsAndValidation(t *testing.T) {
	t.Run("factory explicitly selects legacy", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		require.Equal(t, messageKeyFormatLegacy, cfg.MessageKey.Format)
	})

	t.Run("omitted and empty format remain legacy", func(t *testing.T) {
		for _, format := range []string{"", messageKeyFormatLegacy} {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = "https://example.service-now.com"
			cfg.MessageKey.Format = format
			require.NoError(t, cfg.Validate())
		}
	})

	t.Run("empty format decodes and validates as legacy", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.ClientConfig.Endpoint = "https://example.service-now.com"
		conf := confmap.NewFromStringMap(map[string]any{
			"message_key": map[string]any{"format": ""},
		})
		require.NoError(t, conf.Unmarshal(cfg))
		require.Empty(t, cfg.MessageKey.Format)
		require.NoError(t, cfg.Validate())
	})

	t.Run("unknown format is rejected", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.ClientConfig.Endpoint = "https://example.service-now.com"
		cfg.MessageKey.Format = "future"
		require.ErrorContains(t, cfg.Validate(), "message_key.format")
	})

	t.Run("v1 minimum digest budget", func(t *testing.T) {
		for _, limit := range []int{79, 1024, 1025} {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = "https://example.service-now.com"
			cfg.MessageKey.Format = messageKeyFormatSHA256V1
			cfg.MessageKey.Separator = "::"
			cfg.FieldLimits.MessageKey = limit
			require.NoError(t, cfg.Validate())
		}

		cfg := createDefaultConfig().(*Config)
		cfg.ClientConfig.Endpoint = "https://example.service-now.com"
		cfg.MessageKey.Format = messageKeyFormatSHA256V1
		cfg.FieldLimits.MessageKey = 78
		require.ErrorContains(t, cfg.Validate(), "field_limits.message_key")
	})

	t.Run("legacy retains custom positive budgets", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.ClientConfig.Endpoint = "https://example.service-now.com"
		cfg.MessageKey.Format = messageKeyFormatLegacy
		cfg.FieldLimits.MessageKey = 1
		require.NoError(t, cfg.Validate())
	})
}
