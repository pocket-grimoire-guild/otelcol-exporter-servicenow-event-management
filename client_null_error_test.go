// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumererror"
)

func TestSummarizeServiceNowNullAndAbsentErrorFields(t *testing.T) {
	for _, body := range []struct {
		name string
		body string
	}{
		{name: "top-level errors absent", body: `{}`},
		{name: "record errors absent", body: `{"records":[{}]}`},
	} {
		t.Run(body.name, func(t *testing.T) {
			summary, err := summarizeServiceNow2xxJSONResponse([]byte(body.body))
			require.NoError(t, err)
			require.Empty(t, summary)
		})
	}

	for _, field := range []string{"error", "_error", "__error"} {
		for _, value := range []struct {
			name string
			json string
		}{
			{name: "null", json: `null`},
			{name: "whitespace around null", json: " \n\t null \t\n "},
		} {
			t.Run("top-level/"+field+"/"+value.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"%s":%s}`, field, value.json))
				summary, err := summarizeServiceNow2xxJSONResponse(body)
				require.NoError(t, err)
				require.Empty(t, summary)
			})
		}
	}

	for _, field := range []string{"__error", "_error"} {
		for _, value := range []struct {
			name string
			json string
		}{
			{name: "null", json: `null`},
			{name: "whitespace around null", json: " \n\t null \t\n "},
		} {
			t.Run("record/"+field+"/"+value.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"records":[{"%s":%s}]}`, field, value.json))
				summary, err := summarizeServiceNow2xxJSONResponse(body)
				require.NoError(t, err)
				require.Empty(t, summary)
			})
		}
	}
}

func TestSummarizeServiceNowNonNullErrorFieldsRemainFailures(t *testing.T) {
	values := []struct {
		name string
		json string
	}{
		{name: "object", json: `{"message":"private detail"}`},
		{name: "string null", json: `"null"`},
		{name: "empty object", json: `{}`},
	}
	for _, field := range []string{"error", "_error", "__error"} {
		for _, value := range values {
			t.Run("top-level/"+field+"/"+value.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"%s":%s}`, field, value.json))
				summary, err := summarizeServiceNow2xxJSONResponse(body)
				require.NoError(t, err)
				require.NotEmpty(t, summary)
			})
		}
	}

	for _, field := range []string{"__error", "_error"} {
		for _, value := range values {
			t.Run("record/"+field+"/"+value.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"records":[{"%s":%s}]}`, field, value.json))
				summary, err := summarizeServiceNow2xxJSONResponse(body)
				require.NoError(t, err)
				require.NotEmpty(t, summary)
			})
		}
	}
}

func TestSummarizeServiceNowNullErrorsDoNotMaskFailureStatuses(t *testing.T) {
	for _, field := range []string{"status", "_status", "__status"} {
		t.Run("top-level/"+field, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"%s":"failure","error":null,"_error":null,"__error":null}`, field))
			summary, err := summarizeServiceNow2xxJSONResponse(body)
			require.NoError(t, err)
			require.Contains(t, summary, "top_level_failure=true")
		})
	}

	for _, field := range []string{"__status", "_status"} {
		t.Run("record/"+field, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"records":[{"%s":"failure","__error":null,"_error":null}]}`, field))
			summary, err := summarizeServiceNow2xxJSONResponse(body)
			require.NoError(t, err)
			require.Contains(t, summary, "failed_records=1")
		})
	}
}

func TestSummarizeServiceNowGenericErrorEnvelopeGuardsRemainUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		wantFailure bool
	}{
		{name: "unscoped error", body: `{"error":{"message":"private detail"}}`, wantFailure: true},
		{name: "null result counts as present", body: `{"error":{"message":"private detail"},"result":null}`},
		{name: "result object counts as present", body: `{"error":{},"result":{"count":1}}`},
		{name: "nonempty records count as present", body: `{"error":{},"records":[{}]}`},
		{name: "empty records retain empty-envelope behavior", body: `{"error":{},"records":[]}`, wantFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary, err := summarizeServiceNow2xxJSONResponse([]byte(tc.body))
			require.NoError(t, err)
			if tc.wantFailure {
				require.NotEmpty(t, summary)
			} else {
				require.Empty(t, summary)
			}
		})
	}
}

func TestFactoryAcceptsNullServiceNowErrorFieldsAcrossSupportedRoutes(t *testing.T) {
	responseBodies := []string{
		`{"status":"success","error":null,"_error":null,"__error":null}`,
		`{"records":[{"__status":"success","__error":null,"_error":null}]}`,
	}

	for _, route := range serviceNowRoutes {
		t.Run(route.mode+"/"+route.api, func(t *testing.T) {
			var attempts atomic.Int32
			requests := make(chan struct {
				path  string
				query string
			}, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := int(attempts.Add(1)) - 1
				select {
				case requests <- struct {
					path  string
					query string
				}{path: r.URL.Path, query: r.URL.RawQuery}:
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if attempt >= len(responseBodies) {
					attempt = len(responseBodies) - 1
				}
				_, _ = w.Write([]byte(responseBodies[attempt]))
			}))
			defer server.Close()

			cfg := retryTestConfig(server.URL)
			cfg.Mode = route.mode
			cfg.API = route.api
			cfg.ClientConfig.Timeout = 500 * time.Millisecond
			cfg.BackOffConfig.MaxElapsedTime = 50 * time.Millisecond
			logsExporter := startRetryFactoryExporter(t, cfg, nil)
			for range responseBodies {
				require.NoError(t, logsExporter.ConsumeLogs(t.Context(), sampleLogs()))
				select {
				case got := <-requests:
					require.Equal(t, route.path, got.path)
					require.Equal(t, route.rawQuery, got.query)
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for the HTTP request")
				}
			}
			require.Equal(t, int32(len(responseBodies)), attempts.Load())
		})
	}
}

func TestFactoryAcceptsKnownServiceNowSuccessShapesAcrossSupportedRoutes(t *testing.T) {
	for _, tc := range []struct {
		route serviceNowRoute
		body  string
	}{
		{route: serviceNowRoutes[0], body: `{"result":{"count":1}}`},
		{route: serviceNowRoutes[1], body: `{"records":[{"__status":"success","sys_id":"168160ad4a36231200a89091281dc803"}]}`},
		{route: serviceNowRoutes[2]},
		{route: serviceNowRoutes[3]},
	} {
		t.Run(tc.route.mode+"/"+tc.route.api, func(t *testing.T) {
			var attempts atomic.Int32
			requests := make(chan struct {
				path  string
				query string
			}, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				select {
				case requests <- struct {
					path  string
					query string
				}{path: r.URL.Path, query: r.URL.RawQuery}:
				default:
				}
				if tc.body != "" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer server.Close()

			cfg := retryTestConfig(server.URL)
			cfg.Mode = tc.route.mode
			cfg.API = tc.route.api
			cfg.ClientConfig.Timeout = 500 * time.Millisecond
			cfg.BackOffConfig.MaxElapsedTime = 50 * time.Millisecond
			logsExporter := startRetryFactoryExporter(t, cfg, nil)
			require.NoError(t, logsExporter.ConsumeLogs(t.Context(), sampleLogs()))
			require.Equal(t, int32(1), attempts.Load())
			select {
			case got := <-requests:
				require.Equal(t, tc.route.path, got.path)
				require.Equal(t, tc.route.rawQuery, got.query)
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for the HTTP request")
			}
		})
	}
}

func TestFactoryKeepsServiceNowFailuresPermanentWithNullErrorPeers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		wantSummary string
		secret      string
	}{
		{
			name:        "top-level failure status with null errors",
			body:        `{"status":"failure","error":null,"_error":null,"__error":null}`,
			wantSummary: "top_level_failure=true",
		},
		{
			name:        "record failure status with null errors",
			body:        `{"records":[{"__status":"failure","__error":null,"_error":null}]}`,
			wantSummary: "failed_records=1",
		},
		{
			name:        "top-level actual error with null peers",
			body:        `{"error":null,"_error":{"message":"PRIVATE_TOP_DETAIL"},"__error":null}`,
			wantSummary: "top_level_failure=true",
			secret:      "PRIVATE_TOP_DETAIL",
		},
		{
			name:        "record actual error with null peer",
			body:        `{"records":[{"__error":null,"_error":{"message":"PRIVATE_RECORD_DETAIL"}}]}`,
			wantSummary: "failed_records=1",
			secret:      "PRIVATE_RECORD_DETAIL",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			cfg := retryTestConfig(server.URL)
			cfg.ClientConfig.Timeout = 500 * time.Millisecond
			logsExporter := startRetryFactoryExporter(t, cfg, nil)
			err := logsExporter.ConsumeLogs(t.Context(), sampleLogs())
			require.Error(t, err)
			require.True(t, consumererror.IsPermanent(err))
			require.Contains(t, err.Error(), tc.wantSummary)
			require.Contains(t, err.Error(), "status=200")
			if tc.secret != "" {
				require.NotContains(t, err.Error(), tc.secret)
			}
			require.Equal(t, int32(1), attempts.Load())
		})
	}
}
