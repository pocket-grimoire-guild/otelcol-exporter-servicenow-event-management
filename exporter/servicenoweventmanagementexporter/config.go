// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	ModeInstance = "instance"
	ModeMID      = "mid"

	APIJSONV2        = "jsonv2"
	APIBusinessRules = "business_rules"

	defaultAdditionalInfoMaxAttributes  = 128
	defaultAdditionalInfoMaxValueLength = 4000
	defaultMessageKeySeparator          = "|"
)

const (
	serviceNowMaxSource          = 200
	serviceNowMaxEventClass      = 100
	serviceNowMaxNode            = 100
	serviceNowMaxResource        = 100
	serviceNowMaxMetricName      = 1024
	serviceNowMaxType            = 100
	serviceNowMaxMessageKey      = 1024
	serviceNowMaxDescription     = 4000
	serviceNowMaxAdditionalInfo  = 4000
	serviceNowMaxResolutionState = 40
)

const (
	serviceNowResolutionStateNew     = "New"
	serviceNowResolutionStateClosing = "Closing"
)

var defaultRedactAttributePatterns = []string{
	"*authorization*",
	"*api_key*",
	"*apikey*",
	"*client_secret*",
	"*cookie*",
	"*credential*",
	"*password*",
	"*secret*",
	"*session*",
	"*token*",
}

// Config defines the ServiceNow Event Management exporter configuration.
type Config struct {
	ClientConfig confighttp.ClientConfig `mapstructure:",squash"`

	Mode       string `mapstructure:"mode"`
	API        string `mapstructure:"api"`
	Source     string `mapstructure:"source"`
	EventClass string `mapstructure:"event_class"`
	EventType  string `mapstructure:"type"`

	MessageKey     MessageKeyConfig     `mapstructure:"message_key"`
	Severity       SeverityConfig       `mapstructure:"severity"`
	FieldLimits    FieldLimitsConfig    `mapstructure:"field_limits"`
	AdditionalInfo AdditionalInfoConfig `mapstructure:"additional_info"`

	QueueSettings configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`
	BackOffConfig configretry.BackOffConfig                                `mapstructure:"retry_on_failure"`
}

type MessageKeyConfig struct {
	Attributes []string `mapstructure:"attributes"`
	Separator  string   `mapstructure:"separator"`
}

type SeverityConfig struct {
	FromAttribute        string                `mapstructure:"from_attribute"`
	Default              int                   `mapstructure:"default"`
	Mapping              SeverityMappingConfig `mapstructure:"mapping"`
	ClearResolutionState string                `mapstructure:"clear_resolution_state"`
}

type SeverityMappingConfig struct {
	Fatal int `mapstructure:"fatal"`
	Error int `mapstructure:"error"`
	Warn  int `mapstructure:"warn"`
	Info  int `mapstructure:"info"`
}

type FieldLimitsConfig struct {
	Source          int `mapstructure:"source"`
	EventClass      int `mapstructure:"event_class"`
	Node            int `mapstructure:"node"`
	Resource        int `mapstructure:"resource"`
	MetricName      int `mapstructure:"metric_name"`
	Type            int `mapstructure:"type"`
	MessageKey      int `mapstructure:"message_key"`
	Description     int `mapstructure:"description"`
	AdditionalInfo  int `mapstructure:"additional_info"`
	ResolutionState int `mapstructure:"resolution_state"`
}

type AdditionalInfoConfig struct {
	IncludeAttributes []string `mapstructure:"include_attributes"`
	ExcludeAttributes []string `mapstructure:"exclude_attributes"`
	RedactAttributes  []string `mapstructure:"redact_attributes"`
	MaxAttributes     int      `mapstructure:"max_attributes"`
	MaxValueLength    int      `mapstructure:"max_value_length"`
}

func (cfg *Config) Validate() error {
	if strings.TrimSpace(cfg.ClientConfig.Endpoint) == "" {
		return errors.New("endpoint is required")
	}

	parsed, err := url.Parse(cfg.ClientConfig.Endpoint)
	if err != nil {
		return fmt.Errorf("endpoint must be a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("endpoint scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("endpoint must include a host")
	}

	switch cfg.Mode {
	case ModeInstance, ModeMID:
	default:
		return fmt.Errorf("mode must be %q or %q, got %q", ModeInstance, ModeMID, cfg.Mode)
	}

	switch cfg.eventAPI() {
	case APIJSONV2, APIBusinessRules:
	default:
		return fmt.Errorf("api must be %q or %q, got %q", APIJSONV2, APIBusinessRules, cfg.API)
	}

	if cfg.Severity.Default < 0 || cfg.Severity.Default > 5 {
		return fmt.Errorf("severity.default must be between 0 and 5, got %d", cfg.Severity.Default)
	}
	if err := cfg.Severity.Validate(); err != nil {
		return err
	}

	if cfg.ClientConfig.Timeout < 0 {
		return errors.New("'timeout' must be non-negative")
	}
	if err := cfg.ClientConfig.Validate(); err != nil {
		return err
	}
	if err := cfg.MessageKey.Validate(); err != nil {
		return err
	}
	if err := cfg.FieldLimits.Validate(); err != nil {
		return err
	}
	if err := cfg.AdditionalInfo.Validate(); err != nil {
		return err
	}
	if cfg.QueueSettings.HasValue() {
		if err := cfg.QueueSettings.Validate(); err != nil {
			return err
		}
	}
	if err := cfg.BackOffConfig.Validate(); err != nil {
		return err
	}

	return nil
}

func (cfg *Config) eventAPI() string {
	if cfg.API == "" {
		return APIJSONV2
	}
	return cfg.API
}

func NewDefaultFieldLimitsConfig() FieldLimitsConfig {
	return FieldLimitsConfig{
		Source:          serviceNowMaxSource,
		EventClass:      serviceNowMaxEventClass,
		Node:            serviceNowMaxNode,
		Resource:        serviceNowMaxResource,
		MetricName:      serviceNowMaxMetricName,
		Type:            serviceNowMaxType,
		MessageKey:      serviceNowMaxMessageKey,
		Description:     serviceNowMaxDescription,
		AdditionalInfo:  serviceNowMaxAdditionalInfo,
		ResolutionState: serviceNowMaxResolutionState,
	}
}

func NewDefaultAdditionalInfoConfig() AdditionalInfoConfig {
	return AdditionalInfoConfig{
		RedactAttributes: append([]string(nil), defaultRedactAttributePatterns...),
		MaxAttributes:    defaultAdditionalInfoMaxAttributes,
		MaxValueLength:   defaultAdditionalInfoMaxValueLength,
	}
}

func (cfg MessageKeyConfig) Validate() error {
	if cfg.Separator != "" && strings.TrimSpace(cfg.Separator) == "" {
		return errors.New("message_key.separator cannot be only whitespace")
	}
	return nil
}

func (cfg SeverityConfig) Validate() error {
	levels := map[string]int{
		"severity.mapping.fatal": cfg.Mapping.Fatal,
		"severity.mapping.error": cfg.Mapping.Error,
		"severity.mapping.warn":  cfg.Mapping.Warn,
		"severity.mapping.info":  cfg.Mapping.Info,
	}
	for name, value := range levels {
		if value < 0 || value > 5 {
			return fmt.Errorf("%s must be between 0 and 5, got %d", name, value)
		}
	}
	switch cfg.ClearResolutionState {
	case "", serviceNowResolutionStateNew, serviceNowResolutionStateClosing:
	default:
		return fmt.Errorf("severity.clear_resolution_state must be empty, %q, or %q, got %q", serviceNowResolutionStateNew, serviceNowResolutionStateClosing, cfg.ClearResolutionState)
	}
	return nil
}

func (cfg FieldLimitsConfig) Validate() error {
	limits := map[string]int{
		"field_limits.source":           cfg.Source,
		"field_limits.event_class":      cfg.EventClass,
		"field_limits.node":             cfg.Node,
		"field_limits.resource":         cfg.Resource,
		"field_limits.metric_name":      cfg.MetricName,
		"field_limits.type":             cfg.Type,
		"field_limits.message_key":      cfg.MessageKey,
		"field_limits.description":      cfg.Description,
		"field_limits.additional_info":  cfg.AdditionalInfo,
		"field_limits.resolution_state": cfg.ResolutionState,
	}
	for name, value := range limits {
		if value <= 0 {
			return fmt.Errorf("%s must be positive, got %d", name, value)
		}
	}
	if cfg.AdditionalInfo < 256 {
		return fmt.Errorf("field_limits.additional_info must be at least 256, got %d", cfg.AdditionalInfo)
	}
	return nil
}

func (cfg AdditionalInfoConfig) Validate() error {
	if cfg.MaxAttributes < 0 {
		return fmt.Errorf("additional_info.max_attributes must be non-negative, got %d", cfg.MaxAttributes)
	}
	if cfg.MaxValueLength <= 0 {
		return fmt.Errorf("additional_info.max_value_length must be positive, got %d", cfg.MaxValueLength)
	}
	if err := validateGlobPatterns("additional_info.include_attributes", cfg.IncludeAttributes); err != nil {
		return err
	}
	if err := validateGlobPatterns("additional_info.exclude_attributes", cfg.ExcludeAttributes); err != nil {
		return err
	}
	if err := validateGlobPatterns("additional_info.redact_attributes", cfg.RedactAttributes); err != nil {
		return err
	}
	return nil
}

func validateGlobPatterns(name string, patterns []string) error {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return fmt.Errorf("%s cannot contain empty patterns", name)
		}
		if _, err := path.Match(pattern, "servicenow-event-management-validation-probe"); err != nil {
			return fmt.Errorf("%s contains invalid glob pattern %q: %w", name, pattern, err)
		}
	}
	return nil
}
