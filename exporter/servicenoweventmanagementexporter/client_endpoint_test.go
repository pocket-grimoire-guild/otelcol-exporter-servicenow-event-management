// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceNowEndpointRejectsModePathMismatch(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		mode     string
		api      string
	}{
		{
			name:     "instance path with MID mode",
			endpoint: "https://example.service-now.com/api/global/em/jsonv2",
			mode:     ModeMID,
			api:      APIJSONV2,
		},
		{
			name:     "MID path with instance mode",
			endpoint: "https://example.service-now.com/api/mid/em/jsonv2",
			mode:     ModeInstance,
			api:      APIJSONV2,
		},
		{
			name:     "business rules path with JSON v2 API",
			endpoint: "https://example.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple",
			mode:     ModeInstance,
			api:      APIJSONV2,
		},
		{
			name:     "instance path with business rules API",
			endpoint: "https://example.service-now.com/api/global/em/jsonv2",
			mode:     ModeInstance,
			api:      APIBusinessRules,
		},
		{
			name:     "business rules path with MID mode",
			endpoint: "https://example.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple",
			mode:     ModeMID,
			api:      APIBusinessRules,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = tt.endpoint
			cfg.Mode = tt.mode
			cfg.API = tt.api

			_, err := serviceNowEndpoint(cfg)
			require.ErrorContains(t, err, "does not match mode")
		})
	}
}

func TestServiceNowEndpointUsesFullMatchingModePath(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		mode     string
		api      string
	}{
		{
			name:     "instance",
			endpoint: "https://example.service-now.com/api/global/em/jsonv2",
			mode:     ModeInstance,
			api:      APIJSONV2,
		},
		{
			name:     "MID",
			endpoint: "https://example.service-now.com/api/mid/em/jsonv2",
			mode:     ModeMID,
			api:      APIJSONV2,
		},
		{
			name:     "instance business rules",
			endpoint: "https://example.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple",
			mode:     ModeInstance,
			api:      APIBusinessRules,
		},
		{
			name:     "MID business rules",
			endpoint: "https://example.service-now.com/api/mid/em/jsonv2",
			mode:     ModeMID,
			api:      APIBusinessRules,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = tt.endpoint
			cfg.Mode = tt.mode
			cfg.API = tt.api

			endpoint, err := serviceNowEndpoint(cfg)
			require.NoError(t, err)
			require.Equal(t, cfg.ClientConfig.Endpoint, endpoint)
		})
	}
}

func TestServiceNowEndpointRejectsUnsupportedJSONV2Path(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com/custom/jsonv2"
	cfg.Mode = ModeInstance

	_, err := serviceNowEndpoint(cfg)
	require.ErrorContains(t, err, "not a supported")
}

func TestServiceNowEndpointAppendsModePathToProxyBase(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode string
		api  string
		want string
	}{
		{
			name: "MID JSON v2",
			mode: ModeMID,
			api:  APIJSONV2,
			want: "https://proxy.example.net/servicenow/api/mid/em/jsonv2",
		},
		{
			name: "instance business rules",
			mode: ModeInstance,
			api:  APIBusinessRules,
			want: "https://proxy.example.net/servicenow/em_event.do?JSONv2&sysparm_action=insertMultiple",
		},
		{
			name: "MID business rules",
			mode: ModeMID,
			api:  APIBusinessRules,
			want: "https://proxy.example.net/servicenow/api/mid/em/jsonv2",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = "https://proxy.example.net/servicenow"
			cfg.Mode = tt.mode
			cfg.API = tt.api

			endpoint, err := serviceNowEndpoint(cfg)
			require.NoError(t, err)
			require.Equal(t, tt.want, endpoint)
		})
	}
}

func TestServiceNowEndpointAddsBusinessRulesQuery(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com/em_event.do"
	cfg.Mode = ModeInstance
	cfg.API = APIBusinessRules

	endpoint, err := serviceNowEndpoint(cfg)
	require.NoError(t, err)
	require.Equal(t, "https://example.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple", endpoint)
}

func TestServiceNowEndpointRejectsBusinessRulesQueryMismatch(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com/em_event.do?JSONv2&sysparm_action=insert"
	cfg.Mode = ModeInstance
	cfg.API = APIBusinessRules

	_, err := serviceNowEndpoint(cfg)
	require.ErrorContains(t, err, "endpoint query")
}

func TestServiceNowEndpointRejectsBusinessRulesExtraQueryParameters(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com/em_event.do?JSONv2&sysparm_action=insertMultiple&tenant=prod"
	cfg.Mode = ModeInstance
	cfg.API = APIBusinessRules

	_, err := serviceNowEndpoint(cfg)
	require.ErrorContains(t, err, "endpoint query")
}

func TestServiceNowEndpointAcceptsBusinessRulesQueryOrder(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = "https://example.service-now.com/em_event.do?sysparm_action=insertMultiple&JSONv2"
	cfg.Mode = ModeInstance
	cfg.API = APIBusinessRules

	endpoint, err := serviceNowEndpoint(cfg)
	require.NoError(t, err)
	require.Equal(t, cfg.ClientConfig.Endpoint, endpoint)
}

func TestServiceNowEndpointRejectsUnsupportedQueryParameters(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		mode     string
		api      string
	}{
		{
			name:     "base URL",
			endpoint: "https://example.service-now.com?tenant=prod",
			mode:     ModeInstance,
			api:      APIJSONV2,
		},
		{
			name:     "proxy base URL",
			endpoint: "https://proxy.example.net/servicenow?tenant=prod",
			mode:     ModeMID,
			api:      APIJSONV2,
		},
		{
			name:     "full JSON v2 URL",
			endpoint: "https://example.service-now.com/api/global/em/jsonv2?tenant=prod",
			mode:     ModeInstance,
			api:      APIJSONV2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = tt.endpoint
			cfg.Mode = tt.mode
			cfg.API = tt.api

			_, err := serviceNowEndpoint(cfg)
			require.ErrorContains(t, err, "endpoint query parameters are not supported")
		})
	}
}
