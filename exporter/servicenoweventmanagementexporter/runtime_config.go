// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"fmt"

	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

type runtimeConfig struct {
	mode         string
	api          string
	route        serviceNowRoute
	endpoint     string
	clientConfig confighttp.ClientConfig
	mapping      mappingConfig
	queue        configoptional.Optional[exporterhelper.QueueBatchConfig]
	retry        configretry.BackOffConfig
}

type mappingConfig struct {
	Source         string
	EventClass     string
	EventType      string
	MessageKey     MessageKeyConfig
	Severity       SeverityConfig
	FieldLimits    FieldLimitsConfig
	AdditionalInfo AdditionalInfoConfig
}

func newRuntimeConfig(cfg *Config) (runtimeConfig, error) {
	if err := cfg.Validate(); err != nil {
		return runtimeConfig{}, err
	}

	api := cfg.eventAPI()
	route, ok := serviceNowRouteFor(cfg.Mode, api)
	if !ok {
		return runtimeConfig{}, fmt.Errorf("unsupported ServiceNow route mode=%q api=%q", cfg.Mode, api)
	}
	endpoint, err := serviceNowEndpointFor(cfg.ClientConfig.Endpoint, route)
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		mode:         cfg.Mode,
		api:          api,
		route:        route,
		endpoint:     endpoint,
		clientConfig: cfg.ClientConfig,
		mapping:      newMappingConfig(cfg),
		queue:        cfg.QueueSettings,
		retry:        cfg.BackOffConfig,
	}, nil
}

func newMappingConfig(cfg *Config) mappingConfig {
	return mappingConfig{
		Source:     cfg.Source,
		EventClass: cfg.EventClass,
		EventType:  cfg.EventType,
		MessageKey: MessageKeyConfig{
			Attributes: cloneStringSlice(cfg.MessageKey.Attributes),
			Separator:  cfg.MessageKey.Separator,
		},
		Severity:    cfg.Severity,
		FieldLimits: cfg.FieldLimits,
		AdditionalInfo: AdditionalInfoConfig{
			IncludeAttributes: cloneStringSlice(cfg.AdditionalInfo.IncludeAttributes),
			ExcludeAttributes: cloneStringSlice(cfg.AdditionalInfo.ExcludeAttributes),
			RedactAttributes:  cloneStringSlice(cfg.AdditionalInfo.RedactAttributes),
			MaxAttributes:     cfg.AdditionalInfo.MaxAttributes,
			MaxValueLength:    cfg.AdditionalInfo.MaxValueLength,
		},
	}
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}
