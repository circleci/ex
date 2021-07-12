package datadog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestClient_Validate(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantValid bool
		wantErr   string
	}{
		{
			name: "Valid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if !correctURL(w, r, "/api/v1/validate") {
					return
				}
				_, _ = fmt.Fprintln(w, `{"valid": true}`)
			},
			wantValid: true,
		},
		{
			name: "Invalid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintln(w, `{"valid": false}`)
			},
			wantValid: false,
		},
		{
			name: "GarbageResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintln(w, `{garbage JSON}`)
			},
			wantValid: false,
			wantErr:   "failed to decode response",
		},
		{
			name: "GoodErrorResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintln(w, `{"errors": ["error1", "error2"]}`)
			},
			wantValid: false,
			wantErr:   "datadog HTTP error: [error1 error2]",
		},
		{
			name: "GarbageErrorResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintln(w, `{garbage JSON}`)
			},
			wantValid: false,
			wantErr:   "failed to decode error",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			c := testSetup(t, tt.handler)

			gotValid, err := c.Validate(ctx)

			if len(tt.wantErr) == 0 {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}

			assert.Check(t, cmp.Equal(gotValid, tt.wantValid))
		})
	}
}

func TestClient_QueryMetrics(t *testing.T) {
	t1 := time.Unix(0, 1430311800000*1e6)
	t2 := time.Unix(0, 1430312999000*1e6)

	ctx := context.Background()
	validMetadata := ResponseMetadata{
		RateLimit: RateLimit{
			Limit:     600,
			Period:    time.Hour,
			Remaining: 597,
			Reset:     time.Hour - 10*time.Second,
		},
	}
	emptySeries := QueryResponse{Meta: validMetadata}

	tests := []struct {
		name     string
		args     QueryParams
		handler  http.HandlerFunc
		wantResp QueryResponse
		wantErr  string
	}{
		{
			name: "Valid",
			args: QueryParams{From: t1, To: t2, Query: "a valid query"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if !correctURL(w, r, "/api/v1/query?from=1430311800&query=a+valid+query&to=1430312999") {
					return
				}
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `
{
  "status": "ok",
  "res_type": "time_series",
  "series": [
    {
      "metric": "system.cpu.idle",
      "attributes": {},
      "display_name": "system.cpu.idle",
      "unit": null,
      "pointlist": [
        [
          1430311800000,
          98.19375610351562
        ],
        [
          1430312400000,
          99.85856628417969
        ]
      ],
      "end": 1430312999000,
      "interval": 600,
      "start": 1430311800000,
      "length": 2,
      "aggr": null,
      "scope": "host:vagrant-ubuntu-trusty-64",
      "expression": "system.cpu.idle{host:vagrant-ubuntu-trusty-64}"
    }
  ],
  "from_date": 1430226140000,
  "group_by": [
    "host"
  ],
  "to_date": 1430312540000,
  "query": "system.cpu.idle{*}by{host}",
  "message": ""
}`)
			},
			wantResp: QueryResponse{
				Series: []Series{
					{
						Metric:      "system.cpu.idle",
						DisplayName: "system.cpu.idle",
						Points: []DataPoint{
							{Time: UnixTime{time.Unix(0, 1430311800000*1e6)}, Value: 98.19375610351562},
							{Time: UnixTime{time.Unix(0, 1430312400000*1e6)}, Value: 99.85856628417969},
						},
						Start:      UnixTime{t1},
						End:        UnixTime{t2},
						Interval:   600,
						Length:     2,
						Scope:      "host:vagrant-ubuntu-trusty-64",
						Expression: "system.cpu.idle{host:vagrant-ubuntu-trusty-64}"},
				},
				Meta: validMetadata,
			},
		},
		{
			name: "ParseMetadata",
			args: QueryParams{From: t1, To: t2, Query: "valid"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `{"status": "ok"}`)
			},
			wantResp: emptySeries,
		},
		{
			name: "ParseMetadataOnError",
			args: QueryParams{From: t1, To: t2, Query: "valid"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-RateLimit-Limit", "1500")
				w.Header().Add("X-RateLimit-Period", "3600")
				w.Header().Add("X-RateLimit-Remaining", "1493")
				w.Header().Add("X-RateLimit-Reset", "60")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(&errorsResponse{
					Errors: []string{"rate limit quote exceeded"},
				})
			},
			wantResp: QueryResponse{
				Meta: ResponseMetadata{
					RateLimit: RateLimit{
						Limit:     1500,
						Period:    time.Hour,
						Remaining: 1493,
						Reset:     time.Minute,
					},
				},
			},
			wantErr: "rate limit quote exceeded",
		},
		{
			name: "BadRequest",
			args: QueryParams{From: t1, To: t2, Query: "a bad request"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(&errorsResponse{
					Errors: []string{"bad request"},
				})
			},
			wantErr:  "datadog HTTP error: [bad request]",
			wantResp: emptySeries,
		},
		{
			name: "InvalidQuery",
			args: QueryParams{From: t1, To: t2, Query: "an invalid query"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `{"status": "error", "error": "the reason"}`)
			},
			wantErr:  "invalid query: the reason",
			wantResp: emptySeries,
		},
		{
			name: "GarbageResponse",
			args: QueryParams{From: t1, To: t2, Query: "something"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `{garbage response}`)
			},
			wantErr:  "failed to decode response",
			wantResp: emptySeries,
		},
		{
			name: "BadTimestamps",
			args: QueryParams{From: t1, To: t2, Query: "something"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `
				{
				  "status": "ok",
				  "series": [
				    {
				      "end": "string instead of numeric"
				    }
				  ]
				}`)
			},
			wantErr:  "failed to decode unix time",
			wantResp: emptySeries,
		},
		{
			name: "BadDataPointTime",
			args: QueryParams{From: t1, To: t2, Query: "something"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `
				{
				  "status": "ok",
				  "series": [
				    {
				      "pointlist": [
				        [
				          "should be a numeric",
				          98.19375610351562
				        ]
				      ]
				    }
				  ]
				}`)
			},
			wantErr:  "failed to decode data point",
			wantResp: emptySeries,
		},
		{
			name: "BadDataPointValue",
			args: QueryParams{From: t1, To: t2, Query: "something"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				addValidRateLimitHeaders(w)
				_, _ = fmt.Fprintln(w, `
				{
				  "status": "ok",
				  "series": [
				    {
				      "pointlist": [
				        [
				          1430311800000,
				          "should be a numeric"
				        ]
				      ]
				    }
				  ]
				}`)
			},
			wantErr:  "failed to decode data point",
			wantResp: emptySeries,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			c := testSetup(t, tt.handler)

			gotResp, err := c.Query(ctx, tt.args)

			if len(tt.wantErr) == 0 {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}

			assert.Check(t, cmp.DeepEqual(gotResp, tt.wantResp))
		})
	}
}

func addValidRateLimitHeaders(w http.ResponseWriter) {
	w.Header().Add("X-RateLimit-Limit", "600")
	w.Header().Add("X-RateLimit-Period", "3600")
	w.Header().Add("X-RateLimit-Remaining", "597")
	w.Header().Add("X-RateLimit-Reset", "3590")
}

func correctURL(w http.ResponseWriter, r *http.Request, expectedURL string) bool {
	if r.URL.String() != expectedURL {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(&errorsResponse{
			Errors: []string{fmt.Sprintf("%q != %q", r.URL, expectedURL)},
		})
		return false
	}
	return true
}

func testSetup(t *testing.T, handler http.HandlerFunc) (client *Client) {
	ts := httptest.NewServer(handler)

	u, _ := url.Parse(ts.URL)
	u.Path = "/api/v1/"

	client = &Client{
		APIKey:  "API Key",
		AppKey:  "Application Key",
		BaseURL: u,
	}
	t.Cleanup(ts.Close)
	return client
}
