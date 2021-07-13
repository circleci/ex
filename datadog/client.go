package datadog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	APIKey     string
	AppKey     string
	BaseURL    *url.URL
	HTTPClient *http.Client
}

var ErrHTTP = errors.New("datadog HTTP error")

func (c *Client) Validate(ctx context.Context) (valid bool, err error) {
	u := &url.URL{Path: "validate"}

	var resp struct {
		IsValid bool `json:"valid"`
	}
	err = c.doGet(ctx, u, &resp, nil)
	if err != nil {
		return false, err
	}

	return resp.IsValid, nil
}

var ErrInvalidQuery = errors.New("invalid query")

type QueryParams struct {
	From, To time.Time
	Query    string
}

type QueryResponse struct {
	Series []Series
	Meta   ResponseMetadata
}

func (c *Client) Query(ctx context.Context, q QueryParams) (response QueryResponse, err error) {
	v := url.Values{}
	v.Add("from", strconv.FormatInt(q.From.Unix(), 10))
	v.Add("to", strconv.FormatInt(q.To.Unix(), 10))
	v.Add("query", q.Query)
	u := &url.URL{Path: "query", RawQuery: v.Encode()}

	var resp struct {
		Status string   `json:"status"`
		Error  string   `json:"error"`
		Series []Series `json:"series,omitempty"`
	}
	err = c.doGet(ctx, u, &resp, &response.Meta)
	if err != nil {
		return response, err
	}

	if resp.Status != "ok" {
		return response, fmt.Errorf("%w: %s", ErrInvalidQuery, resp.Error)
	}

	response.Series = resp.Series
	return response, nil
}

type Series struct {
	Metric      string      `json:"metric"`
	DisplayName string      `json:"display_name"`
	Points      []DataPoint `json:"pointlist"`
	Start       UnixTime    `json:"start"`
	End         UnixTime    `json:"end"`
	Interval    int         `json:"interval"`
	Aggr        string      `json:"aggr"`
	Length      int         `json:"length"`
	Scope       string      `json:"scope"`
	Expression  string      `json:"expression"`
}

// DataPoint is a tuple of UNIX timestamp, value. It has custom
// JSON serialisation to allow variadic decoding of the JSON array.
type DataPoint struct {
	Time  UnixTime
	Value float64
}

func (d DataPoint) String() string {
	return fmt.Sprintf("{Time:%s, Value:%f}", d.Time, d.Value)
}

func (d *DataPoint) UnmarshalJSON(b []byte) (err error) {
	arr := []interface{}{&d.Time, &d.Value}
	err = json.Unmarshal(b, &arr)
	if err != nil {
		return fmt.Errorf("failed to decode data point: %w", err)
	}
	return nil
}

type UnixTime struct {
	time.Time
}

// UnmarshalJSON converts from milliseconds since the epoch
func (t *UnixTime) UnmarshalJSON(b []byte) (err error) {
	ms, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return fmt.Errorf("failed to decode unix time: %w", err)
	}
	t.Time = time.Unix(0, int64(ms*1e6))
	return nil
}

type ResponseMetadata struct {
	RateLimit RateLimit
}

type RateLimit struct {
	Limit     int64         // X-RateLimit-Limit number of requests allowed in a time period
	Period    time.Duration // X-RateLimit-Period length of time in seconds for resets (calendar aligned)
	Remaining int64         // X-RateLimit-Remaining number of allowed requests left in current time period
	Reset     time.Duration // X-RateLimit-Reset time in seconds until next reset
}

func (r *RateLimit) LoadFromHeader(h *http.Header) (err error) {
	var i int64

	// Limit
	i, err = parseIntFromHeader(h, "X-RateLimit-Limit")
	if err != nil {
		return fmt.Errorf("failed to read RateLimit.Limit: %w", ErrHTTP)
	}
	r.Limit = i

	// Period
	i, err = parseIntFromHeader(h, "X-RateLimit-Period")
	if err != nil {
		return fmt.Errorf("failed to read RateLimit.Period: %w", ErrHTTP)
	}
	r.Period = time.Duration(i) * time.Second

	// Remaining
	i, err = parseIntFromHeader(h, "X-RateLimit-Remaining")
	if err != nil {
		return fmt.Errorf("failed to read RateLimit.Remaining: %w", ErrHTTP)
	}
	r.Remaining = i

	// Reset
	i, err = parseIntFromHeader(h, "X-RateLimit-Reset")
	if err != nil {
		return fmt.Errorf("failed to read RateLimit.Reset: %w", ErrHTTP)
	}
	r.Reset = time.Duration(i) * time.Second

	return nil
}

// HTTP implementation details shared between user-facing methods

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

var defaultBaseURL = url.URL{Scheme: "https", Host: "api.datadoghq.com", Path: "/api/v1/"}

func (c *Client) baseURL() *url.URL {
	if c.BaseURL != nil {
		return c.BaseURL
	}
	return &defaultBaseURL
}

type errorsResponse struct {
	Errors []string `json:"errors"`
}

func (c *Client) doGet(ctx context.Context, u *url.URL, value interface{}, meta *ResponseMetadata) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL().ResolveReference(u).String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	return c.doRequest(req, value, meta)
}

func (c *Client) doRequest(req *http.Request, value interface{}, meta *ResponseMetadata) (err error) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("DD-API-KEY", c.APIKey)
	if len(c.AppKey) != 0 {
		req.Header.Set("DD-APPLICATION-KEY", c.AppKey)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if meta != nil {
		err = meta.RateLimit.LoadFromHeader(&resp.Header)
		if err != nil {
			return err
		}
	}

	decoder := json.NewDecoder(resp.Body)

	if resp.StatusCode >= 300 {
		var respError errorsResponse
		err = decoder.Decode(&respError)
		if err != nil {
			return fmt.Errorf("failed to decode error: %w", err)
		}
		return fmt.Errorf("%w: %s", ErrHTTP, respError.Errors)
	}

	err = decoder.Decode(value)
	if err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

func parseIntFromHeader(h *http.Header, name string) (seconds int64, err error) {
	return strconv.ParseInt(h.Get(name), 10, 64)
}
