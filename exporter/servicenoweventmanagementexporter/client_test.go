// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configauth"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/exporter/exportertest"
)

func TestClientSendsJSONV2Payload(t *testing.T) {
	for _, tt := range []struct {
		name     string
		mode     string
		api      string
		path     string
		rawQuery string
	}{
		{name: "instance jsonv2", mode: ModeInstance, api: APIJSONV2, path: "/api/global/em/jsonv2"},
		{name: "mid jsonv2", mode: ModeMID, api: APIJSONV2, path: "/api/mid/em/jsonv2"},
		{name: "instance business rules", mode: ModeInstance, api: APIBusinessRules, path: "/em_event.do", rawQuery: "JSONv2&sysparm_action=insertMultiple"},
		{name: "mid business rules", mode: ModeMID, api: APIBusinessRules, path: "/api/mid/em/jsonv2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			received := make(chan eventPayload, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, tt.path, r.URL.Path)
				require.Equal(t, tt.rawQuery, r.URL.RawQuery)
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var payload eventPayload
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				received <- payload
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = server.URL
			cfg.Mode = tt.mode
			cfg.API = tt.api

			client := startClient(t, cfg, nil)
			err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry", Severity: "2"}}})
			require.NoError(t, err)

			payload := <-received
			require.Len(t, payload.Records, 1)
			require.Equal(t, "2", payload.Records[0].Severity)
		})
	}
}

func TestClientUsesCollectorAuthExtension(t *testing.T) {
	authID := component.NewIDWithName(component.MustNewType("testauth"), "client")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer from-extension", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.ClientConfig.Auth = configoptional.Some(configauth.Config{AuthenticatorID: authID})

	client := startClient(t, cfg, map[component.ID]component.Component{
		authID: &authHeaderExtension{header: "Bearer from-extension"},
	})
	require.NoError(t, client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}}))
}

func TestClientUsesCollectorHTTPHeadersForMIDAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "key mid-secret", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.ClientConfig.Headers = configopaque.MapList{{Name: "Authorization", Value: configopaque.String("key mid-secret")}}

	client := startClient(t, cfg, nil)
	require.NoError(t, client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}}))
}
func TestClientRequiresStart(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com"

	client, err := newServiceNowClient(cfg, exportertest.NewNopSettings(Type).TelemetrySettings)
	require.NoError(t, err)
	require.ErrorContains(t, client.sendPayload(t.Context(), eventPayload{}), "not started")
}

func TestClientStartSurfacesAuthExtensionErrors(t *testing.T) {
	authID := component.NewIDWithName(component.MustNewType("testauth"), "missing")
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com"
	cfg.ClientConfig.Auth = configoptional.Some(configauth.Config{AuthenticatorID: authID})

	client, err := newServiceNowClient(cfg, exportertest.NewNopSettings(Type).TelemetrySettings)
	require.NoError(t, err)
	require.Error(t, client.start(context.Background(), hostWithExtensions{}))
}
