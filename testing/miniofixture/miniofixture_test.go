package miniofixture

import (
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

	var fix *Fixture
	assert.Assert(t, t.Run("Check bucket is created", func(t *testing.T) {
		fix = Setup(ctx, t, Config{
			Key:    "minio",
			Secret: "minio123",
			URL:    "http://localhost:9123",
		})
		assert.Check(t, len(fix.Bucket) > 0)

		t.Run("Upload object", func(t *testing.T) {
			_, err := fix.Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: &fix.Bucket,
				Key:    aws.String("the-key.txt"),
				Body:   strings.NewReader("the-body"),
			})
			assert.Assert(t, err)
		})
	}))

	t.Run("Check bucket is deleted", func(t *testing.T) {
		_, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &fix.Bucket,
			Key:    aws.String("the-key.txt"),
		})
		assert.Check(t, cmp.ErrorContains(err, "NoSuchBucket"))
	})
}
