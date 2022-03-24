package cloudenv

import (
	"context"
	"fmt"
	"time"

	"github.com/circleci/ex/httpclient"
)

type metadataClient interface {
	ExternalID(ctx context.Context) (externalID string, err error)
	PublicIP(ctx context.Context) (publicIP string, err error)
}

func NewMetadataClient(cfg Config) (metadataClient, error) {
	clientConfig := httpclient.Config{
		Name:    "machine-agent",
		Timeout: 5 * time.Second,
	}
	switch cfg.Provider {
	case ProviderEC2:
		clientConfig.BaseURL = cfg.testMetadataBaseURL("http://169.254.169.254")
		return &ec2MetadataClient{
			client: httpclient.New(clientConfig),
		}, nil
	case ProviderGCE:
		clientConfig.BaseURL = cfg.testMetadataBaseURL("http://metadata.google.internal")
		return &gceMetadataClient{
			client: httpclient.New(clientConfig),
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider: %q", cfg.Provider)
	}
}

type ec2MetadataClient struct {
	client *httpclient.Client
}

func (c *ec2MetadataClient) ExternalID(ctx context.Context) (externalID string, err error) {
	err = c.client.Call(ctx, httpclient.NewRequest("GET", "/latest/meta-data/instance-id",
		httpclient.StringDecoder(&externalID),
	))
	return externalID, err
}

func (c *ec2MetadataClient) PublicIP(ctx context.Context) (publicIP string, err error) {
	err = c.client.Call(ctx, httpclient.NewRequest("GET", "/latest/meta-data/public-ipv4",
		httpclient.StringDecoder(&publicIP),
	))
	return publicIP, err
}

type gceMetadataClient struct {
	client *httpclient.Client
}

func (c *gceMetadataClient) ExternalID(ctx context.Context) (externalID string, err error) {
	err = c.client.Call(ctx, httpclient.NewRequest("GET", "/computeMetadata/v1/instance/id",
		httpclient.Header("Metadata-Flavor", "Google"),
		httpclient.StringDecoder(&externalID),
	))
	return externalID, err
}

func (c *gceMetadataClient) PublicIP(ctx context.Context) (publicIP string, err error) {
	err = c.client.Call(ctx, httpclient.NewRequest(
		"GET", "/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip",
		httpclient.Header("Metadata-Flavor", "Google"),
		httpclient.StringDecoder(&publicIP),
	))
	return publicIP, err
}
