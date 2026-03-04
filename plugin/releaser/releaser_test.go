package releaser_test

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/plugin/releaser"
	"github.com/circleci/ex/testing/miniofixture"
	"github.com/circleci/ex/testing/testcontext"
)

func TestPlugin_Releaser(t *testing.T) {
	ctx := testcontext.Background()
	fix := miniofixture.Default(ctx, t)

	plugin := "fake"
	version := "1.0.0"

	for _, namespace := range []releaser.Namespace{
		releaser.NamespacePlugin,
		releaser.NamespaceSubcommand,
	} {
		t.Run(namespace.String(), func(t *testing.T) {
			r, err := releaser.New(releaser.Config{
				Plugin:  plugin,
				Version: version,

				Bucket:    fix.Bucket,
				Namespace: namespace,
				Client:    fix.Client,
			})
			assert.NilError(t, err)

			err = r.Run(ctx, releaser.Opts{
				Source:     "github.com/circleci/ex/plugin/releaser/internal/cmd",
				WorkingDir: filepath.Join("..", ".."),
			})
			assert.NilError(t, err)

			t.Run("correct-release-version", func(t *testing.T) {
				key := path.Join(namespace.String(), plugin, "release.txt")
				resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: &fix.Bucket,
					Key:    &key,
				})
				assert.NilError(t, err)

				b, err := io.ReadAll(resp.Body)
				assert.NilError(t, err)
				assert.Check(t, string(b) == version)
			})

			t.Run("binaries-exist", func(t *testing.T) {
				tests := []struct {
					os       string
					expected []string
				}{
					{
						os: "darwin",
						expected: []string{
							"amd64/fake",
							"arm64/fake",
						},
					},
					{
						os: "linux",
						expected: []string{
							"amd64/fake",
							"arm/fake",
							"arm64/fake",
							"ppc64le/fake",
							"s390x/fake",
						},
					},
					{
						os: "windows",
						expected: []string{
							"amd64/fake.exe",
							"arm64/fake.exe",
						},
					},
				}

				for _, tt := range tests {
					t.Run(tt.os, func(t *testing.T) {
						prefix := path.Join(namespace.String(), plugin, version, tt.os)
						resp, err := fix.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
							Bucket: &fix.Bucket,
							Prefix: aws.String(prefix),
						})
						assert.NilError(t, err)

						for _, c := range resp.Contents {
							key := strings.TrimPrefix(*c.Key, prefix+"/")
							assert.Check(t, contains(tt.expected, key), "could not find key %q", *c.Key)
						}
					})
				}
			})

			t.Run("correct-checksum", func(t *testing.T) {
				prefix := path.Join(namespace.String(), plugin, version)
				resp, err := fix.Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: &fix.Bucket,
					Key:    aws.String(path.Join(prefix, "checksums.txt")),
				})
				assert.NilError(t, err)
				defer func() {
					_ = resp.Body.Close()
				}()

				checksum := checksum(resp.Body, runtime.GOOS, runtime.GOARCH, plugin)
				assert.Check(t, checksum != "")

				resp, err = fix.Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: &fix.Bucket,
					Key:    aws.String(path.Join(prefix, runtime.GOOS, runtime.GOARCH, plugin)),
				})
				assert.NilError(t, err)
				defer func() {
					_ = resp.Body.Close()
				}()

				gz, err := gzip.NewReader(resp.Body)
				assert.NilError(t, err)

				h := sha256.New()
				//#nosec:G110 // This is a test
				_, err = io.Copy(h, gz)
				assert.NilError(t, err)
				actual := fmt.Sprintf("%x", h.Sum(nil))

				assert.Check(t, actual == checksum)
			})
		})
	}
}

func checksum(input io.Reader, os, arch, plugin string) string {
	platform := path.Join(os, arch, plugin)
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		s := strings.SplitN(scanner.Text(), " ", 2)
		path := strings.TrimPrefix(s[1], "*")
		if path == platform {
			return s[0]
		}
	}
	return ""
}

func contains(haystack []string, needle string) bool {
	for _, el := range haystack {
		if el == needle {
			return true
		}
	}

	return false
}
