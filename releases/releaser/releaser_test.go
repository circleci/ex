package releaser

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/golden"

	"github.com/circleci/ex/testing/miniofixture"
)

func TestReleaser_Publish(t *testing.T) {
	ctx := context.Background()

	fix := miniofixture.Setup(ctx, t, miniofixture.Config{
		Key:    "minio",
		Secret: "minio123",
		URL:    "http://localhost:9123",
	})

	r := NewWithClient(fix.Client)

	assert.Assert(t, t.Run("Publish", func(t *testing.T) {
		err := r.Publish(ctx, PublishParameters{
			Path:    filepath.Join("testdata", "target", "bin"),
			Bucket:  fix.Bucket,
			App:     "app",
			Version: "0.0.1-dev",
		})
		assert.Assert(t, err)
	}))

	checksums := ""

	assert.Assert(t, t.Run("Check checksums is published", func(t *testing.T) {
		resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &fix.Bucket,
			Key:    aws.String("app/0.0.1-dev/checksums.txt"),
		})
		assert.Assert(t, err)
		defer resp.Body.Close()

		b, err := ioutil.ReadAll(resp.Body)
		assert.Assert(t, err)
		checksums = string(b)

		assert.Check(t, golden.String(checksums, "expected-checksums.txt"))
	}))

	type entry struct {
		Checksum string
		Path     string
	}
	var checksumEntries []entry

	assert.Assert(t, t.Run("Parse checksums", func(t *testing.T) {
		scanner := bufio.NewScanner(strings.NewReader(checksums))
		for scanner.Scan() {
			s := strings.SplitN(scanner.Text(), " ", 2)
			assert.Assert(t, cmp.Len(s, 2))
			checksumEntries = append(checksumEntries, entry{
				Checksum: s[0],
				Path:     strings.TrimPrefix(s[1], "*"),
			})
		}
		assert.Assert(t, scanner.Err())
	}))

	t.Run("Verify checksums", func(t *testing.T) {
		for _, e := range checksumEntries {
			t.Run(e.Path, func(t *testing.T) {
				resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: &fix.Bucket,
					Key:    aws.String("app/0.0.1-dev/" + e.Path),
				})
				assert.Assert(t, err)
				defer resp.Body.Close()

				gz, err := gzip.NewReader(resp.Body)
				assert.Assert(t, err)

				h := sha256.New()
				//#nosec:G110 // This is a test
				_, err = io.Copy(h, gz)
				assert.Assert(t, err)
				assert.Assert(t, gz.Close())
				assert.Check(t, cmp.Equal(e.Checksum, fmt.Sprintf("%x", h.Sum(nil))))
			})
		}
	})
}

func TestReleaser_Release(t *testing.T) {
	ctx := context.Background()

	fix := miniofixture.Setup(ctx, t, miniofixture.Config{
		Key:    "minio",
		Secret: "minio123",
		URL:    "http://localhost:9123",
	})

	r := NewWithClient(fix.Client)

	assert.Assert(t, t.Run("Release", func(t *testing.T) {
		err := r.Release(ctx, ReleaseParameters{
			Bucket:  fix.Bucket,
			App:     "app",
			Version: "0.0.1-dev",
		})
		assert.Assert(t, err)
	}))

	t.Run("Check released", func(t *testing.T) {
		resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &fix.Bucket,
			Key:    aws.String("app/release.txt"),
		})
		assert.Assert(t, err)
		defer resp.Body.Close()

		b, err := ioutil.ReadAll(resp.Body)
		assert.Assert(t, err)

		assert.Check(t, cmp.Equal(string(b), "0.0.1-dev"))
	})
}
