// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pipeline"
)

func TestFactoryDefaultsAndSupportedSignals(t *testing.T) {
	factory := NewFactory()

	require.Equal(t, component.MustNewType("servicenow_event_management"), factory.Type())
	require.Equal(t, component.StabilityLevelDevelopment, factory.LogsStability())

	cfg, ok := factory.CreateDefaultConfig().(*Config)
	require.True(t, ok)
	require.Equal(t, ModeInstance, cfg.Mode)
	require.Equal(t, APIJSONV2, cfg.API)
	require.Equal(t, "opentelemetry", cfg.Source)
	require.Equal(t, "otel-log", cfg.EventType)
	require.Equal(t, 30*time.Second, cfg.ClientConfig.Timeout)
	require.False(t, cfg.ClientConfig.Auth.HasValue())
	require.Equal(t, 200, cfg.FieldLimits.Source)
	require.Equal(t, 100, cfg.FieldLimits.EventClass)
	require.Equal(t, 100, cfg.FieldLimits.Node)
	require.Equal(t, 100, cfg.FieldLimits.Resource)
	require.Equal(t, 1024, cfg.FieldLimits.MetricName)
	require.Equal(t, 100, cfg.FieldLimits.Type)
	require.Equal(t, 1024, cfg.FieldLimits.MessageKey)
	require.Equal(t, 4000, cfg.FieldLimits.Description)
	require.Equal(t, 4000, cfg.FieldLimits.AdditionalInfo)
	require.Equal(t, 40, cfg.FieldLimits.ResolutionState)
	require.Equal(t, defaultAdditionalInfoMaxAttributes, cfg.AdditionalInfo.MaxAttributes)
	require.Contains(t, cfg.AdditionalInfo.RedactAttributes, "*password*")
	require.Equal(t, 1, cfg.Severity.Mapping.Fatal)
	require.Equal(t, 2, cfg.Severity.Mapping.Error)
	require.Equal(t, 4, cfg.Severity.Mapping.Warn)
	require.Equal(t, 5, cfg.Severity.Mapping.Info)
	require.Equal(t, "Closing", cfg.Severity.ClearResolutionState)
	require.True(t, cfg.QueueSettings.HasValue())
	require.False(t, cfg.QueueSettings.Get().Batch.HasValue())
	require.True(t, cfg.BackOffConfig.Enabled)

	settings := exportertest.NewNopSettings(component.MustNewType("servicenow_event_management"))
	_, err := factory.CreateMetrics(context.Background(), settings, cfg)
	require.ErrorIs(t, err, pipeline.ErrSignalNotSupported)
	_, err = factory.CreateTraces(context.Background(), settings, cfg)
	require.ErrorIs(t, err, pipeline.ErrSignalNotSupported)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "missing endpoint",
			wantErr: "endpoint",
		},
		{
			name: "unsupported mode",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.Mode = "table-api"
			},
			wantErr: "mode",
		},
		{
			name: "unsupported api",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.API = "table-api"
			},
			wantErr: "api",
		},
		{
			name: "invalid timeout",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.ClientConfig.Timeout = -1 * time.Second
			},
			wantErr: "timeout",
		},
		{
			name: "additional info field limit too small",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.FieldLimits.AdditionalInfo = 128
			},
			wantErr: "field_limits.additional_info",
		},
		{
			name: "invalid redact pattern",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.AdditionalInfo.RedactAttributes = []string{"["}
			},
			wantErr: "additional_info.redact_attributes",
		},
		{
			name: "invalid severity mapping",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.Severity.Mapping.Error = 7
			},
			wantErr: "severity.mapping.error",
		},
		{
			name: "invalid clear resolution state",
			mutate: func(cfg *Config) {
				cfg.ClientConfig.Endpoint = "https://example.service-now.com"
				cfg.Severity.ClearResolutionState = "Closed"
			},
			wantErr: "severity.clear_resolution_state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			require.ErrorContains(t, cfg.Validate(), tt.wantErr)
		})
	}
}

func TestConfigValidationAcceptsDefaultsWithEndpoint(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com"

	require.NoError(t, cfg.Validate())
}

func TestConfigValidationRejectsMalformedEndpoint(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://[::1"
	require.ErrorContains(t, cfg.Validate(), "valid URL")

	cfg.ClientConfig.Endpoint = "ftp://example.service-now.com"
	require.ErrorContains(t, cfg.Validate(), "scheme")

	cfg.ClientConfig.Endpoint = "https:///api/global/em/jsonv2"
	require.ErrorContains(t, cfg.Validate(), "host")
}

func TestConfigValidationRejectsNestedConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "message key whitespace separator",
			mutate: func(cfg *Config) {
				cfg.MessageKey.Separator = " \t "
			},
			wantErr: "message_key.separator",
		},
		{
			name: "negative additional info max attributes",
			mutate: func(cfg *Config) {
				cfg.AdditionalInfo.MaxAttributes = -1
			},
			wantErr: "additional_info.max_attributes",
		},
		{
			name: "zero additional info max value length",
			mutate: func(cfg *Config) {
				cfg.AdditionalInfo.MaxValueLength = 0
			},
			wantErr: "additional_info.max_value_length",
		},
		{
			name: "invalid include pattern",
			mutate: func(cfg *Config) {
				cfg.AdditionalInfo.IncludeAttributes = []string{"["}
			},
			wantErr: "additional_info.include_attributes",
		},
		{
			name: "invalid exclude pattern",
			mutate: func(cfg *Config) {
				cfg.AdditionalInfo.ExcludeAttributes = []string{"["}
			},
			wantErr: "additional_info.exclude_attributes",
		},
		{
			name: "non-positive field limit",
			mutate: func(cfg *Config) {
				cfg.FieldLimits.Node = 0
			},
			wantErr: "field_limits.node",
		},
		{
			name: "invalid queue size",
			mutate: func(cfg *Config) {
				cfg.QueueSettings.Get().QueueSize = 0
			},
			wantErr: "queue_size",
		},
		{
			name: "invalid retry interval",
			mutate: func(cfg *Config) {
				cfg.BackOffConfig.InitialInterval = -1 * time.Second
			},
			wantErr: "initial_interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = "https://example.service-now.com"
			tt.mutate(cfg)
			require.ErrorContains(t, cfg.Validate(), tt.wantErr)
		})
	}
}

func TestCreateLogsExporterValidatesConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)

	_, err := createLogsExporter(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.ErrorContains(t, err, "endpoint")
}
