// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRuntimeConfigNormalizesDefaultsAndRoute(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com/proxy"
	cfg.API = ""

	runtime, err := newRuntimeConfig(cfg)
	require.NoError(t, err)

	require.Equal(t, ModeInstance, runtime.mode)
	require.Equal(t, APIJSONV2, runtime.api)
	require.Equal(t, instanceJSONV2Path, runtime.route.path)
	require.Equal(t, "https://example.service-now.com/proxy/api/global/em/jsonv2", runtime.endpoint)

	cfg.API = APIBusinessRules
	require.Equal(t, APIJSONV2, runtime.api)
	require.Equal(t, instanceJSONV2Path, runtime.route.path)
}

func TestNewRuntimeConfigCopiesMappingSlices(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com"
	cfg.MessageKey.Attributes = []string{"service.name"}
	cfg.AdditionalInfo.IncludeAttributes = []string{"safe.*"}
	cfg.AdditionalInfo.ExcludeAttributes = []string{"drop.*"}
	cfg.AdditionalInfo.RedactAttributes = []string{"secret.*"}

	runtime, err := newRuntimeConfig(cfg)
	require.NoError(t, err)

	cfg.MessageKey.Attributes[0] = "host.name"
	cfg.AdditionalInfo.IncludeAttributes[0] = "unsafe.*"
	cfg.AdditionalInfo.ExcludeAttributes[0] = "keep.*"
	cfg.AdditionalInfo.RedactAttributes[0] = "public.*"

	require.Equal(t, []string{"service.name"}, runtime.mapping.MessageKey.Attributes)
	require.Equal(t, []string{"safe.*"}, runtime.mapping.AdditionalInfo.IncludeAttributes)
	require.Equal(t, []string{"drop.*"}, runtime.mapping.AdditionalInfo.ExcludeAttributes)
	require.Equal(t, []string{"secret.*"}, runtime.mapping.AdditionalInfo.RedactAttributes)
}

func TestRuntimeConfigSnapshotsMessageKeyFormatAndAttributes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com"
	cfg.MessageKey.Format = messageKeyFormatSHA256V1
	cfg.MessageKey.Attributes = []string{"service.name", "host.name"}

	runtime, err := newRuntimeConfig(cfg)
	require.NoError(t, err)

	cfg.MessageKey.Format = messageKeyFormatLegacy
	cfg.MessageKey.Attributes[0] = "event.name"

	require.Equal(t, messageKeyFormatSHA256V1, runtime.mapping.MessageKey.Format)
	require.Equal(t, []string{"service.name", "host.name"}, runtime.mapping.MessageKey.Attributes)
}
