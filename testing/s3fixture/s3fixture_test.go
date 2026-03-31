package s3fixture

import (
	"context"
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
			Key:    "seaweed",
			Secret: "seaweed123",
			Region: "us-east-2",
			URL:    "http://localhost:9123",
		}
		testSetup(t, ctx, fix)
	})

	t.Run("default", func(t *testing.T) {
		fix := Default(ctx, t)

		t.Run("is-versioned", func(t *testing.T) {
			assert.Check(t, fix.Versioned)
		})
	})

	t.Run("specific-bucket", func(t *testing.T) {
		fix := &Fixture{
			Bucket: "a-specific-bucket-seaweed",
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
