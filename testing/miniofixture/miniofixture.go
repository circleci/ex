package miniofixture

import (
	"context"
	"errors"
	"fmt"
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

// Fixture can be used to set up fields that will be used in creating
type Fixture struct {
	Client             *s3.Client
	URL                string
	Key                secret.String
	Secret             secret.String
	Bucket             string
	Region             string
	Versioned          bool // Versioned will be set true if we have managed to enable versioning
	ForceLocal         bool // ForceLocal will fail on a local run if minio is not running
	ForceVersioned     bool // ForceVersioned will fail if the bucket can not be set versioned
	DisallowPublicRead bool // DisallowPublicRead will not set permissions on the bucket for direct access to links
}

// Default sets up and returns the default minio fixture
func Default(ctx context.Context, t testing.TB) *Fixture {
	fix := &Fixture{}
	Setup(ctx, t, fix)
	return fix
}

// Setup will take the given fixture adding default values as needed and update the fields in the fixture
// with whatever values were used.
func Setup(ctx context.Context, t testing.TB, fix *Fixture) {
	t.Helper()
	setConfigDefaults(t, fix)
	skipIfNotRunning(t, fix)

	assert.Assert(t, fix.Client == nil, "fixture client is expected to be nil")

	err := runSetup(ctx, fix)
	assert.NilError(t, err)

	t.Cleanup(func() {
		fix.clean(t)
	})
}

func runSetup(ctx context.Context, fix *Fixture) error {
	awsConfig := newAWSConfig(fix)
	fix.Client = s3.NewFromConfig(awsConfig)

	_, err := fix.Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(fix.Bucket),
	})
	if err != nil {
		return fmt.Errorf("create bucket failed: %w", err)
	}

	_, err = fix.Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(fix.Bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: "Enabled",
		},
	})

	fix.Versioned = err == nil

	if err != nil {
		fix.Versioned = false
		if fix.ForceVersioned {
			return fmt.Errorf("forced bucket versioning failed: %w", err)
		}
	}

	if !fix.DisallowPublicRead {
		_, err = fix.Client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
			Bucket: aws.String(fix.Bucket),
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

		if err != nil {
			return fmt.Errorf("could not create public read policy: %w", err)
		}
	}

	return nil
}

func setConfigDefaults(t testing.TB, fix *Fixture) {
	if fix.URL == "" {
		fix.URL = "http://localhost:9123"
	}
	if fix.Key == "" {
		fix.Key = "minio"
	}
	if fix.Secret == "" {
		fix.Secret = "minio123"
	}
	if fix.Bucket == "" {
		fix.Bucket = BucketName(t)
	}
	if fix.Region == "" {
		fix.Region = "us-east-1"
	}
}

func skipIfNotRunning(t testing.TB, fix *Fixture) {
	t.Helper()
	if fix.ForceLocal {
		return
	}
	if strings.EqualFold("true", strings.ToLower(os.Getenv("CI"))) {
		return
	}

	u, err := url.Parse(fix.URL)
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

func newAWSConfig(fix *Fixture) aws.Config {
	resolveFn := func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			PartitionID:       "aws",
			URL:               fix.URL,
			SigningRegion:     fix.Region,
			HostnameImmutable: true,
		}, nil
	}

	return aws.Config{
		Region:                      fix.Region,
		Credentials:                 credentials.NewStaticCredentialsProvider(fix.Key.Value(), fix.Secret.Value(), ""),
		EndpointResolverWithOptions: aws.EndpointResolverWithOptionsFunc(resolveFn),
	}
}

func (f *Fixture) clean(t testing.TB) {
	t.Helper()
	ctx := context.Background()

	var err error
	for i := 0; i < 5; i++ {
		if f.Versioned {
			f.emptyVersionedBucket(ctx, t)
		} else {
			f.emptyBucket(ctx, t)
		}
		_, err = f.Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: &f.Bucket,
		})
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	assert.NilError(t, err)
}

func (f *Fixture) emptyVersionedBucket(ctx context.Context, t testing.TB) {
	listReq := &s3.ListObjectVersionsInput{Bucket: &f.Bucket}
	for {
		out, err := f.Client.ListObjectVersions(ctx, listReq)
		if err != nil {
			// Check if already deleted
			e := &types.NoSuchBucket{}
			if errors.As(err, &e) {
				return
			}
			t.Fatalf("Failed to list objects: %v", err)
		}

		for _, ver := range out.Versions {
			f.deleteS3Object(ctx, t, ver)
		}

		// if objects have been deleted they may leave delete markers lying around
		for _, dm := range out.DeleteMarkers {
			f.deleteS3Object(ctx, t, types.ObjectVersion{
				Key:       dm.Key,
				VersionId: dm.VersionId,
			})
		}

		if aws.ToBool(out.IsTruncated) {
			listReq.KeyMarker = out.NextKeyMarker
			listReq.VersionIdMarker = out.NextVersionIdMarker
		} else {
			break
		}
	}
}

func (f *Fixture) emptyBucket(ctx context.Context, t testing.TB) {
	listReq := &s3.ListObjectsInput{Bucket: &f.Bucket}
	for {
		out, err := f.Client.ListObjects(ctx, listReq)
		if err != nil {
			// Check if already deleted
			e := &types.NoSuchBucket{}
			if errors.As(err, &e) {
				return
			}
			t.Fatalf("Failed to list objects: %v", err)
		}

		for _, o := range out.Contents {
			f.deleteS3Object(ctx, t, types.ObjectVersion{
				Key: o.Key,
			})
		}

		if aws.ToBool(out.IsTruncated) {
			listReq.Marker = out.NextMarker
		} else {
			break
		}
	}
}

func (f *Fixture) deleteS3Object(ctx context.Context, t testing.TB, ver types.ObjectVersion) {
	_, err := f.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(f.Bucket),
		Key:       ver.Key,
		VersionId: ver.VersionId,
	})
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}
}
