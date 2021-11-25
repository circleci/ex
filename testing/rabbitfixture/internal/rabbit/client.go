package rabbit

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"time"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/httpclient"
)

type Client struct {
	client *httpclient.Client
}

type VHostInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func NewClient(baseURL, username string, password secret.String) *Client {
	return &Client{client: httpclient.New(httpclient.Config{
		Name:       "rabbitmq",
		BaseURL:    baseURL,
		AuthHeader: "Authorization",
		AuthToken:  "Basic " + basicAuth(username, password),
		Timeout:    30 * time.Second,
	})}
}

func basicAuth(username string, password secret.String) string {
	auth := username + ":" + password.Value()
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func (c *Client) ListVHosts(ctx context.Context) (info []VHostInfo, err error) {
	err = c.client.Call(ctx, httpclient.Request{
		Method:  "GET",
		Route:   "/api/vhosts",
		Decoder: httpclient.NewJSONDecoder(&info),
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (c *Client) DeleteVHost(ctx context.Context, name string) (err error) {
	err = c.client.Call(ctx, httpclient.NewRequest(
		"DELETE",
		"/api/vhosts/%s",
		time.Second*5,
		url.PathEscape(name),
	))
	if httpclient.HasStatusCode(err, http.StatusNotFound) {
		return nil
	}
	if httpclient.IsNoContent(err) {
		return nil
	}
	return err
}

type VHostSettings struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Tracing     bool     `json:"tracing"`
}

func (c *Client) PutVHost(ctx context.Context, name string, settings VHostSettings) (err error) {
	req := httpclient.NewRequest(
		"PUT",
		"/api/vhosts/%s",
		time.Second*5,
		url.PathEscape(name),
	)
	req.Body = settings
	return c.client.Call(ctx, req)
}

type Permissions struct {
	Configure string `json:"configure"`
	Write     string `json:"write"`
	Read      string `json:"read"`
}

func (c *Client) UpdatePermissionsIn(ctx context.Context, vhost, username string, p Permissions) (err error) {
	req := httpclient.NewRequest(
		"PUT",
		"/api/permissions/%s/%s",
		time.Second*5,
		url.PathEscape(vhost),
		url.PathEscape(username),
	)
	req.Body = p
	return c.client.Call(ctx, req)
}
