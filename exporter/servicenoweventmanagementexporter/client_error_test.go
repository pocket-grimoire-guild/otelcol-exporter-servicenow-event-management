// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumererror"
)

func TestClientClassifiesHTTPFailures(t *testing.T) {
	tests := []struct {
		status        int
		wantPermanent bool
	}{
		{status: http.StatusBadRequest, wantPermanent: true},
		{status: http.StatusRequestTimeout, wantPermanent: false},
		{status: http.StatusUnauthorized, wantPermanent: true},
		{status: http.StatusTooManyRequests, wantPermanent: false},
		{status: http.StatusInternalServerError, wantPermanent: false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "service-now says no", tt.status)
			}))
			defer server.Close()

			cfg := createDefaultConfig().(*Config)
			cfg.ClientConfig.Endpoint = server.URL
			cfg.Mode = ModeMID

			client := startClient(t, cfg, nil)
			err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
			require.Error(t, err)
			require.Equal(t, tt.wantPermanent, consumererror.IsPermanent(err))
			require.NotContains(t, err.Error(), "Authorization")
		})
	}
}

func TestClientOmitsErrorResponseBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"Authorization Bearer secret-token rejected","description":"customer payload"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "response body omitted for data safety")
	require.NotContains(t, err.Error(), "secret-token")
	require.NotContains(t, err.Error(), "Authorization Bearer")
	require.NotContains(t, err.Error(), "customer payload")
}

func TestClientIncludesSafeServiceNowErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":{"code":"em.invalid_event","message":"echoed customer payload"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "error_code=em.invalid_event")
	require.NotContains(t, err.Error(), "echoed customer payload")
}

func TestClientOmitsUnsafeServiceNowErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":{"code":"unsafe customer payload with spaces"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "unsafe customer payload")
	require.NotContains(t, err.Error(), "error_code=")
}

func TestClientSummarizesEmptyErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty response body")
}

func TestClientAcceptsServiceNow2xxRecordSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"records":[{"__status":"success","sys_id":"168160ad4a36231200a89091281dc803"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	require.NoError(t, client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}}))
}

func TestClientTreatsServiceNow2xxRecordFailureAsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"records": [
				{"__status":"success","sys_id":"168160ad4a36231200a89091281dc803"},
				{
					"__status":"failure",
					"__error":{"message":"Invalid Insert into: em_event","reason":"Data Policy Exception: customer payload"}
				}
			]
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "status=200")
	require.Contains(t, err.Error(), "failed_records=1")
	require.Contains(t, err.Error(), "response body omitted for data safety")
	require.NotContains(t, err.Error(), "customer payload")
	require.NotContains(t, err.Error(), "Invalid Insert")
}

func TestClientTreatsServiceNow2xxTopLevelFailureAsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"_status":"failure",
			"_error":{"message":"Cannot update with empty sysparm_query","reason":"customer payload"}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "top_level_failure=true")
	require.Contains(t, err.Error(), "response body omitted for data safety")
	require.NotContains(t, err.Error(), "customer payload")
	require.NotContains(t, err.Error(), "Cannot update")
}

func TestClientTreatsServiceNow2xxRESTFailureAsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"status":"failure",
			"error":{"code":"em.invalid_event","message":"customer payload"}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "top_level_failure=true")
	require.Contains(t, err.Error(), "error_code=em.invalid_event")
	require.NotContains(t, err.Error(), "customer payload")
}

func TestClientRejectsNonJSON2xxResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`<html><title>ServiceNow instance is hibernating</title></html>`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeInstance

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "unexpected non-JSON 2xx response")
	require.NotContains(t, err.Error(), "hibernating")
}

func TestClientRejectsMalformedJSON2xxResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"records":[`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeInstance

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.True(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "invalid JSON 2xx response")
}

func TestSummarizeSuccessResponseRejectsOversizedBodies(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxSuccessResponseBytes+maxErrorResponseDrainBytes+2))
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(body),
	}

	summary := summarizeSuccessResponse(resp)
	require.Contains(t, summary, "too large to verify")
	require.Equal(t, 1, body.Len())
}

func TestSummarizeErrorResponseDrainsBoundedLargeBodies(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxErrorResponseBytes+maxErrorResponseDrainBytes+1))
	resp := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(body),
	}

	summary := summarizeErrorResponse(resp)
	require.Equal(t, "response body omitted for data safety", summary)
	require.Equal(t, 1, body.Len())
}

func TestClientHonorsRetryAfterDelaySeconds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "Throttle (30s)")
}

func TestClientHonorsRetryAfterHTTPDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", time.Now().Add(10*time.Second).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
	require.Contains(t, err.Error(), "Throttle (")
}

func TestClientIgnoresMalformedRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "eventually")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err := client.sendPayload(t.Context(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
	require.NotContains(t, err.Error(), "Throttle (")
}

func TestClientSurfacesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.sendPayload(ctx, eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, consumererror.IsPermanent(err))
}

func TestClientCancelsBlockedHTTPRequest(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		<-releaseHandler
	}))
	defer server.Close()
	defer close(releaseHandler)

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.sendPayload(ctx, eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked ServiceNow request")
	}
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, consumererror.IsPermanent(err))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled ServiceNow request")
	}
}

func TestClientSurfacesNetworkErrorsAsRetryable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = endpoint
	cfg.Mode = ModeMID

	client := startClient(t, cfg, nil)
	err = client.sendPayload(context.Background(), eventPayload{Records: []eventRecord{{Source: "opentelemetry"}}})
	require.Error(t, err)
	require.False(t, consumererror.IsPermanent(err))
}
