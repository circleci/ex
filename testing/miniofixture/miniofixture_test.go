package miniofixture

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func TestSetup(t *testing.T) {
	ctx := testcontext.Background()

	t.Run("some-set", func(t *testing.T) {
		fix := &Fixture{
			Key:    "minio-nv",
			Secret: "minio-nv-123",
			Region: "us-east-2",
			URL:    "http://localhost:9124",
		}
		testSetup(t, ctx, fix)

		t.Run("maybe-is-not-versioned", func(t *testing.T) {
			// something about the file system on mac means that only one volume means that
			// versioning can not be enabled
			if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
				assert.Check(t, !fix.Versioned)
			} else {
				// on other os's the file system appears to support versioning with one volume
				// Minio docs refer to Erasure Coding support.
				// https://min.io/docs/minio/linux/operations/concepts/erasure-coding.html#minio-ec-erasure-set
				assert.Check(t, fix.Versioned)
			}
		})

		t.Run("force-ver-fails", func(t *testing.T) {
			if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
				fix.Client = nil
				fix.ForceVersioned = true
				err := runSetup(ctx, fix)
				assert.ErrorContains(t, err, "failed")
			} else {
				t.Skip("force versioning succeeds on non-intel-mac file systems")
			}
		})
	})

	t.Run("default", func(t *testing.T) {
		fix := Default(ctx, t)

		t.Run("is-versioned", func(t *testing.T) {
			assert.Check(t, fix.Versioned)
		})
	})

	t.Run("specific-bucket", func(t *testing.T) {
		fix := &Fixture{
			Bucket: "a-specific-bucket",
		}
		testSetup(t, ctx, fix)
	})

	t.Run("force-versioned", func(t *testing.T) {
		fix := &Fixture{
			ForceVersioned: true,
		}
		Setup(ctx, t, fix)
	})
}

func testSetup(t *testing.T, ctx context.Context, fix *Fixture) {
	key := aws.String("the-key.txt")

	t.Run("setup-add-delete", func(t *testing.T) {
		Setup(ctx, t, fix)

		assert.Assert(t, t.Run("Check bucket is created", func(t *testing.T) {
			assert.Check(t, len(fix.Bucket) > 0)

			t.Run("Upload object", func(t *testing.T) {
				_, err := fix.Client.PutObject(ctx, &s3.PutObjectInput{
					Bucket: aws.String(fix.Bucket),
					Key:    key,
					Body:   strings.NewReader("the-body"),
				})
				assert.Assert(t, err)

				_, err = fix.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(fix.Bucket),
					Key:    key,
				})
				assert.Assert(t, err)
			})
		}))

	})

	t.Run("Check bucket is deleted", func(t *testing.T) {
		_, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(fix.Bucket),
			Key:    key,
		})
		assert.Check(t, cmp.ErrorContains(err, "NoSuchBucket"))
	})
}
