package miniofixture

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/config/secret"
)

type Config struct {
	Key    secret.String
	Secret secret.String
	URL    string

	// optional
	Bucket string
	Region string
}

type Fixture struct {
	Bucket string
	Client *s3.Client
}

func Setup(ctx context.Context, t testing.TB, cfg Config) *Fixture {
	t.Helper()
	skipIfNotRunning(t, cfg)

	if cfg.Bucket == "" {
		cfg.Bucket = BucketName(t)
	}

	awsConfig := newAWSConfig(cfg)
	c := s3.NewFromConfig(awsConfig)

	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	assert.Assert(t, err)
	_, err = c.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(cfg.Bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: "Enabled",
		},
	})
	assert.Assert(t, err)

	_, err = c.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(cfg.Bucket),
		Policy: aws.String(`
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "PublicRead",
            "Effect": "Allow",
            "Principal": "*",
            "Action": [
                "s3:GetObject",
                "s3:GetObjectVersion"
            ],
            "Resource": [
                "arn:aws:s3:::*"
            ]
        }
    ]
}`)})
	assert.Assert(t, err)
	t.Cleanup(func() {
		clean(t, c, cfg.Bucket)
	})

	return &Fixture{
		Client: c,
		Bucket: cfg.Bucket,
	}
}

func skipIfNotRunning(t testing.TB, cfg Config) {
	t.Helper()
	if strings.EqualFold("true", strings.ToLower(os.Getenv("CI"))) {
		return
	}

	u, err := url.Parse(cfg.URL)
	assert.Assert(t, err)

	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		t.Skip("Minio is not running")
	}
	_ = conn.Close()
}

func BucketName(t testing.TB) string {
	t.Helper()

	r := rand.Uint32() >> 8 //#nosec:G404 // just to avoid matching bucket names in case of failed cleanup
	prefix := strings.ToLower(t.Name())
	prefix = strings.ReplaceAll(prefix, "_", "-")
	prefix = strings.ReplaceAll(prefix, "/", "-")

	// Bucket names are limited to 63 characters. This will truncate the test name to 54 characters and allow for a
	// random suffix of 8 digits (max value of a 24 bit number is 16777216)
	if len(prefix) > 54 {
		prefix = prefix[:54]
	}

	return prefix + "-" + strconv.Itoa(int(r))
}

func newAWSConfig(c Config) aws.Config {
	region := c.Region
	cred := credentials.NewStaticCredentialsProvider(c.Key.Value(), c.Secret.Value(), "")
	//nolint:staticcheck // SA1019 the suggested EndpointResolverWithOptionsFunc doesn't work
	resolver := aws.EndpointResolverFunc(func(service, region string) (aws.Endpoint, error) {
		return aws.Endpoint{
			//PartitionID:       c.Server.Partition,
			URL:               "http://localhost:9123",
			SigningRegion:     region,
			HostnameImmutable: true,
		}, nil
	})
	return aws.Config{
		Region:           region,
		Credentials:      cred,
		EndpointResolver: resolver,
		//HTTPClient:       pooledHTTPClient(),
	}
}

func clean(t testing.TB, c *s3.Client, bucket string) {
	ctx := context.Background()

	var err error
	for i := 0; i < 5; i++ {
		emptyBucket(ctx, t, c, bucket)
		_, err = c.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: &bucket,
		})
		if err == nil {
			break
		}
	}
	assert.NilError(t, err)
}

func emptyBucket(ctx context.Context, t testing.TB, c *s3.Client, bucket string) {
	listReq := &s3.ListObjectVersionsInput{Bucket: &bucket}
	for {
		out, err := c.ListObjectVersions(ctx, listReq)
		if err != nil {
			e := &types.NoSuchBucket{}
			if errors.As(err, &e) {
				return
			}
			t.Fatalf("Failed to list objects: %v", err)
		}

		for _, ver := range out.Versions {
			deleteS3Object(ctx, t, c, &bucket, ver)
		}

		if out.IsTruncated {
			listReq.KeyMarker = out.NextKeyMarker
			listReq.VersionIdMarker = out.NextVersionIdMarker
		} else {
			break
		}
	}
}

func deleteS3Object(ctx context.Context, t testing.TB, c *s3.Client, bucket *string, ver types.ObjectVersion) {
	_, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    bucket,
		Key:       ver.Key,
		VersionId: ver.VersionId,
	})
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}
}
