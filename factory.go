// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/pocket-grimoire-guild/otelcol-exporter-servicenow-event-management/internal/metadata"
)

var Type = metadata.Type

func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		Type,
		createDefaultConfig,
		exporter.WithLogs(createLogsExporter, metadata.LogsStability),
	)
}

func createDefaultConfig() component.Config {
	clientConfig := confighttp.NewDefaultClientConfig()
	clientConfig.Timeout = 30 * time.Second

	return &Config{
		ClientConfig:   clientConfig,
		Mode:           ModeInstance,
		API:            APIJSONV2,
		Source:         "opentelemetry",
		EventType:      "otel-log",
		FieldLimits:    NewDefaultFieldLimitsConfig(),
		AdditionalInfo: NewDefaultAdditionalInfoConfig(),
		QueueSettings:  configoptional.Some(exporterhelper.NewDefaultQueueConfig()),
		BackOffConfig:  configretry.NewDefaultBackOffConfig(),
		MessageKey: MessageKeyConfig{
			Format:    messageKeyFormatLegacy,
			Separator: defaultMessageKeySeparator,
		},
		Severity: SeverityConfig{
			FromAttribute:        "servicenow.severity",
			Default:              5,
			ClearResolutionState: "Closing",
			Mapping: SeverityMappingConfig{
				Fatal: 1,
				Error: 2,
				Warn:  4,
				Info:  5,
			},
		},
	}
}

func createLogsExporter(
	ctx context.Context,
	set exporter.Settings,
	config component.Config,
) (exporter.Logs, error) {
	return createLogsExporterWithClock(ctx, set, config, time.Now)
}

func createLogsExporterWithClock(
	ctx context.Context,
	set exporter.Settings,
	config component.Config,
	now func() time.Time,
) (exporter.Logs, error) {
	cfg := config.(*Config)
	runtime, err := newRuntimeConfig(cfg)
	if err != nil {
		return nil, err
	}
	telemetryBuilder, err := metadata.NewTelemetryBuilder(set.TelemetrySettings)
	if err != nil {
		return nil, err
	}
	client := newServiceNowClientFromRuntime(runtime, set.TelemetrySettings)

	logsExporter, err := exporterhelper.NewLogs(
		ctx,
		set,
		cfg,
		func(ctx context.Context, logs plog.Logs) error {
			payload, diagnostics, err := mapLogsToPayloadWithDiagnostics(logs, runtime.mapping)
			recordMappingDiagnostics(ctx, telemetryBuilder, diagnostics, set.ID.String())
			if err != nil {
				return consumererror.NewPermanent(err)
			}
			if len(payload.Records) == 0 {
				return nil
			}
			return client.sendPayload(ctx, payload)
		},
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		// HTTP client timeout owns request deadlines, following otlphttpexporter.
		exporterhelper.WithTimeout(exporterhelper.TimeoutConfig{Timeout: 0}),
		exporterhelper.WithRetry(runtime.retry),
		exporterhelper.WithQueue(runtime.queue),
		exporterhelper.WithStart(client.start),
		exporterhelper.WithShutdown(client.shutdown),
	)
	if err != nil {
		return nil, err
	}
	return newAdmissionLogsExporter(logsExporter, now), nil
}

func recordMappingDiagnostics(
	ctx context.Context,
	telemetryBuilder *metadata.TelemetryBuilder,
	summary mappingDiagnosticSummary,
	exporterID string,
) {
	for reason, count := range summary.counts {
		if count <= 0 {
			continue
		}
		telemetryBuilder.ExporterServicenowEventManagementMappingDiagnostics.Add(
			ctx,
			count,
			metric.WithAttributes(
				attribute.String("reason", mappingDiagnosticReasonNames[reason]),
				attribute.String("exporter", exporterID),
			),
		)
	}
}
