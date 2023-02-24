package releaser

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/golden"

	"github.com/circleci/ex/closer"
	"github.com/circleci/ex/testing/miniofixture"
)

func TestReleaser_Publish(t *testing.T) {
	ctx := context.Background()

	fix := miniofixture.Default(ctx, t)

	r := NewWithClient(fix.Client)

	tmpDir := fs.NewDir(t, "", fs.FromDir(filepath.Join("testdata", "target")))

	assert.Assert(t, t.Run("Publish", func(t *testing.T) {
		err := r.Publish(ctx, PublishParameters{
			Path:    tmpDir.Join("bin"),
			Bucket:  fix.Bucket,
			App:     "app",
			Version: "0.0.1-dev",

			IncludeFilter: func(path string, info os.FileInfo) bool {
				return strings.HasSuffix(path, "agent") || strings.HasSuffix(path, "agent.exe")
			},

			Tags: map[string]string{"tag-name": "tag-value"},
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
		defer closer.ErrorHandler(resp.Body, &err)

		b, err := io.ReadAll(resp.Body)
		assert.Assert(t, err)
		checksums = string(b)

		assert.Check(t, golden.String(checksums, "expected-checksums.txt"))
	}))

	assert.Assert(t, t.Run("Check checksums are written to disk", func(t *testing.T) {
		b, err := os.ReadFile(tmpDir.Join("bin", "checksums.txt"))
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

	t.Run("Check releases are present", func(t *testing.T) {
		tests := []struct {
			path string
		}{
			{path: "darwin/amd64/agent"},
			{path: "darwin/arm64/agent"},
			{path: "linux/amd64/agent"},
			{path: "linux/arm64/agent"},
			{path: "windows/amd64/agent.exe"},
		}

		assert.Assert(t, cmp.Len(checksumEntries, len(tests)))

		for i, tt := range tests {
			tt := tt
			e := checksumEntries[i]
			t.Run(tt.path, func(t *testing.T) {
				key := aws.String("app/0.0.1-dev/" + e.Path)

				t.Run("Verify checksum", func(t *testing.T) {
					resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
						Bucket: &fix.Bucket,
						Key:    key,
					})
					assert.Assert(t, err)
					defer closer.ErrorHandler(resp.Body, &err)

					gz, err := gzip.NewReader(resp.Body)
					assert.Assert(t, err)

					h := sha256.New()
					//#nosec:G110 // This is a test
					_, err = io.Copy(h, gz)
					assert.Assert(t, err)
					assert.Assert(t, gz.Close())
					assert.Check(t, cmp.Equal(e.Checksum, fmt.Sprintf("%x", h.Sum(nil))))
				})

				t.Run("Check tags are present", func(t *testing.T) {
					tResp, err := fix.Client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
						Bucket: &fix.Bucket,
						Key:    key,
					})
					assert.Assert(t, err)
					assert.Check(t, cmp.DeepEqual(tResp.TagSet, []types.Tag{
						{Key: aws.String("tag-name"), Value: aws.String("tag-value")},
					}, cmpopts.IgnoreUnexported(types.Tag{})))
				})
			})
		}
	})
}

func TestReleaser_Release(t *testing.T) {
	ctx := context.Background()

	fix := miniofixture.Default(ctx, t)

	r := NewWithClient(fix.Client)

	tests := []struct {
		name                string
		environment         string
		expectedEnvironment string
	}{
		{
			name:                "default is release",
			environment:         "",
			expectedEnvironment: "release",
		},
		{
			name:                "can specify environment",
			environment:         "staging",
			expectedEnvironment: "staging",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			assert.Assert(t, t.Run("Release", func(t *testing.T) {
				err := r.Release(ctx, ReleaseParameters{
					Bucket:  fix.Bucket,
					App:     "app",
					Version: "0.0.1-dev",

					Environment: tt.environment,
					Tags:        map[string]string{"tag-name": "the-value"},
				})
				assert.Assert(t, err)
			}))

			key := "app/" + tt.expectedEnvironment + ".txt"

			t.Run("Check released", func(t *testing.T) {
				resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: &fix.Bucket,
					Key:    aws.String(key),
				})
				assert.Assert(t, err)
				defer closer.ErrorHandler(resp.Body, &err)

				b, err := io.ReadAll(resp.Body)
				assert.Assert(t, err)
				assert.Check(t, cmp.Equal(string(b), "0.0.1-dev"))
				assert.Check(t, cmp.Equal(resp.TagCount, int32(1)))
			})

			t.Run("Check tags are present", func(t *testing.T) {
				tResp, err := fix.Client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
					Bucket: &fix.Bucket,
					Key:    aws.String(key),
				})
				assert.Assert(t, err)
				assert.Check(t, cmp.DeepEqual(tResp.TagSet, []types.Tag{
					{Key: aws.String("tag-name"), Value: aws.String("the-value")},
				}, cmpopts.IgnoreUnexported(types.Tag{})))
			})
		})

	}

}
