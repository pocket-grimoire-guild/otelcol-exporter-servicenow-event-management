// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/extension/extensionauth"
)

func startClient(t *testing.T, cfg *Config, extensions map[component.ID]component.Component) *serviceNowClient {
	t.Helper()

	client, err := newServiceNowClient(cfg, exportertest.NewNopSettings(Type).TelemetrySettings)
	require.NoError(t, err)
	require.NoError(t, client.start(context.Background(), hostWithExtensions(extensions)))
	t.Cleanup(func() {
		require.NoError(t, client.shutdown(context.Background()))
	})
	return client
}

func startLogsExporter(t *testing.T, cfg *Config) exporter.Logs {
	t.Helper()

	logsExporter, err := createLogsExporter(context.Background(), exportertest.NewNopSettings(Type), cfg)
	require.NoError(t, err)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	t.Cleanup(func() {
		require.NoError(t, logsExporter.Shutdown(context.Background()))
	})
	return logsExporter
}

type hostWithExtensions map[component.ID]component.Component

func (h hostWithExtensions) GetExtensions() map[component.ID]component.Component {
	return h
}

type authHeaderExtension struct {
	header string
}

var _ component.Component = (*authHeaderExtension)(nil)
var _ extensionauth.HTTPClient = (*authHeaderExtension)(nil)

func (*authHeaderExtension) Start(context.Context, component.Host) error {
	return nil
}

func (*authHeaderExtension) Shutdown(context.Context) error {
	return nil
}

func (e authHeaderExtension) RoundTripper(base http.RoundTripper) (http.RoundTripper, error) {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Authorization", e.header)
		return base.RoundTrip(req)
	}), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
