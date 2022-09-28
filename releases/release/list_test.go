package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/miniofixture"
	"github.com/circleci/ex/testing/testcontext"
)

func TestRequirements_Validate(t *testing.T) {
	tests := []struct {
		name    string
		request Requirements
		wantErr string
	}{
		{
			name: "Valid production release",
			request: Requirements{
				Version:  "1.0.29509-c0e01dbe",
				Platform: "linux",
				Arch:     "amd64",
			},
		},
		{
			name: "Valid development release",
			request: Requirements{
				Version:  "0.0.0-dev-c0e01dbe",
				Platform: "linux",
				Arch:     "amd64",
			},
		},
		{
			name: "Valid canary release",
			request: Requirements{
				Version:  "0.0.0-canary-c0e01dbe",
				Platform: "linux",
				Arch:     "amd64",
			},
		},
		{
			name: "Invalid version with newline",
			request: Requirements{
				Version:  "1.0.29509-c0e01dbe\n",
				Platform: "linux",
				Arch:     "amd64",
			},
			wantErr: "version is invalid",
		},
		{
			name: "Invalid version with non-hex digits",
			request: Requirements{
				Version:  "1.1.1-abcdefgh",
				Platform: "linux",
				Arch:     "amd64",
			},
			wantErr: "version is invalid",
		},
		{
			name: "Platform missing",
			request: Requirements{
				Version: "1.0.29509-c0e01dbe",
				Arch:    "amd64",
			},
			wantErr: "platform is required",
		},
		{
			name: "Arch missing",
			request: Requirements{
				Version:  "1.0.29509-c0e01dbe",
				Platform: "linux",
			},
			wantErr: "arch is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantErr == "" {
				assert.Assert(t, err)
			} else {
				assert.Check(t, cmp.ErrorContains(err, tt.wantErr))
			}
		})
	}
}

func TestList_Latest(t *testing.T) {
	// speed up testing
	initialStoreTimeout = 2 * time.Second

	var tests = []struct {
		name                   string
		baseURL                string
		wantResp               map[string]string
		additionalReleaseTypes []string
		wantHealthCheckToFail  bool
	}{
		{
			name:     "good request",
			wantResp: map[string]string{DefaultReleaseType: "1.1.1-abcdef01"},
		},
		{
			name:                  "forbidden request",
			baseURL:               "/forbidden",
			wantResp:              map[string]string{DefaultReleaseType: ""},
			wantHealthCheckToFail: true,
		},
		{
			name:                  "error request",
			baseURL:               "/oh-no",
			wantResp:              map[string]string{DefaultReleaseType: ""},
			wantHealthCheckToFail: true,
		},
		{
			name:                   "good request for additional release type",
			additionalReleaseTypes: []string{"candidate"},
			wantResp: map[string]string{
				DefaultReleaseType: "1.1.1-abcdef01",
				"candidate":        "2.2.2-fedcba12",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(testcontext.Background(), 300*time.Millisecond)
			defer cancel()

			bucketURL := fixture(ctx, t)
			agentURL := bucketURL + "/agent"
			dl, err := NewList(ctx, "name", "", agentURL+tt.baseURL, tt.additionalReleaseTypes...)
			if tt.wantHealthCheckToFail {
				assert.Check(t, cmp.ErrorContains(err, "failed"))
				assert.Check(t, errors.Is(err, ErrListVersionNotReady))
			} else {
				assert.Assert(t, err)
			}

			_, live, _ := dl.HealthChecks()

			err = live(ctx)
			if tt.wantHealthCheckToFail && !errors.Is(err, ErrListVersionNotReady) {
				t.Errorf("Health check shouldn't be ready")
			}

			for i := 0; i < 5; i++ {
				assert.DeepEqual(t, dl.Latest(), tt.wantResp[DefaultReleaseType])
			}

			for _, releaseType := range tt.additionalReleaseTypes {
				assert.DeepEqual(t, dl.LatestFor(releaseType), tt.wantResp[releaseType])
			}
		})
	}
}

func TestList_Lookup(t *testing.T) {
	var tests = []struct {
		name     string
		req      Requirements
		wantResp *Release
		wantErr  error
	}{
		{
			name: "Good request linux",
			req: Requirements{
				Version:  "1.1.1-abcdef01",
				Platform: "linux",
				Arch:     "amd64",
			},
			wantResp: &Release{
				Checksum: "3dafc2230bd7941b03355a7e3063da7de8c5e38237a3d4e7d5a77013112fcdab",
				URL:      "/1.1.1-abcdef01/linux/amd64/agent",
			},
		},
		{
			name: "Good request windows",
			req: Requirements{
				Version:  "1.1.1-abcdef01",
				Platform: "windows",
				Arch:     "amd64",
			},
			wantResp: &Release{
				Checksum: "3dafc2230bd7941b03355a7e3063da7de8c5e38237a3d4e7d5a77013112fcdab",
				URL:      "/1.1.1-abcdef01/windows/amd64/agent.exe",
			},
		},
		{
			name: "Unknown OS",
			req: Requirements{
				Version:  "1.1.1-abcdef01",
				Platform: "banana",
				Arch:     "amd64",
			},
			wantErr: ErrNotFound,
		},
		{
			name: "Unknown arch",
			req: Requirements{
				Version:  "1.1.1-abcdef01",
				Platform: "darwin",
				Arch:     "bibble",
			},
			wantErr: ErrNotFound,
		},
		{
			name: "Unknown version",
			req: Requirements{
				Version:  "0.0.0-dev-abcdef01",
				Platform: "linux",
				Arch:     "amd64",
			},
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(testcontext.Background())
			defer cancel()

			bucketURL := fixture(ctx, t)
			agentURL := bucketURL + "/agent"
			dl, err := NewList(ctx, "name", "", agentURL)
			assert.Assert(t, err)
			_, live, _ := dl.HealthChecks()
			assert.Assert(t, live(ctx))

			// run this multiple times to get a cache hits
			for i := 0; i < 5; i++ {
				resp, err := dl.Lookup(ctx, tt.req)

				if tt.wantErr == nil {
					assert.Assert(t, err)
				} else {
					assert.Assert(t, errors.Is(err, tt.wantErr), "want error type %v, got %v", tt.wantErr, err)
				}

				if tt.wantResp == nil {
					assert.Check(t, cmp.Nil(resp))
				} else {
					assert.Check(t, cmp.Equal(resp.Checksum, tt.wantResp.Checksum))
					assert.Check(t, cmp.Contains(resp.URL, tt.wantResp.URL))
				}
			}
		})
	}
}

func fixture(ctx context.Context, t *testing.T) string {
	fix := miniofixture.Setup(ctx, t, miniofixture.Config{
		Key:    "minio",
		Secret: "minio123",
		URL:    "http://localhost:9123",
	})
	bu := buckerUploader{
		bucket:   fix.Bucket,
		uploader: manager.NewUploader(fix.Client),
	}
	_, err := bu.upload(ctx, "agent/release.txt", strings.NewReader("1.1.1-abcdef01"))
	assert.Assert(t, err)

	agentFileContent := "the-agent-content"
	sha := sha256.Sum256([]byte(agentFileContent))
	checksums := checksumsFile(sha[:])

	_, err = bu.upload(ctx, "agent/1.1.1-abcdef01/checksums.txt", strings.NewReader(checksums))
	assert.Assert(t, err)

	_, err = bu.upload(ctx, "agent/1.1.1-abcdef01/linux/amd64/agent", strings.NewReader(agentFileContent))
	assert.Assert(t, err)

	_, err = bu.upload(ctx, "agent/1.1.1-abcdef01/windows/amd64/agent.exe", strings.NewReader(agentFileContent))
	assert.Assert(t, err)

	_, err = bu.upload(ctx, "agent/candidate.txt", strings.NewReader("2.2.2-fedcba12"))
	assert.Assert(t, err)

	agentFileContent = "the-candidate-content"
	sha = sha256.Sum256([]byte(agentFileContent))
	checksums = checksumsFile(sha[:])

	_, err = bu.upload(ctx, "agent/2.2.2-fedcba12/checksum.txt", strings.NewReader(checksums))
	assert.Assert(t, err)

	_, err = bu.upload(ctx, "agent/2.2.2-fedcba12/linux/amd64/agent", strings.NewReader(agentFileContent))
	assert.Assert(t, err)

	_, err = bu.upload(ctx, "agent/2.2.2-fedcba12/windows/amd64/agent.exe", strings.NewReader(agentFileContent))
	assert.Assert(t, err)

	return "http://localhost:9123/" + fix.Bucket
}

type buckerUploader struct {
	bucket   string
	uploader *manager.Uploader
}

func (b buckerUploader) upload(ctx context.Context, key string, seeker io.ReadSeeker) (*manager.UploadOutput, error) {
	return b.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
		Body:   seeker,
	})
}

func checksumsFile(checksum []byte) string {
	return fmt.Sprintf(
		`%[1]s *linux/amd64/agent
%[1]s *windows/amd64/agent.exe
`, hex.EncodeToString(checksum))
}

func TestRequirements_QueryParams(t *testing.T) {
	req := &Requirements{
		Version:  "foo",
		Platform: "bar",
		Arch:     "baz",
	}
	params := req.QueryParams()

	assert.Check(t, cmp.DeepEqual(params, map[string]string{
		"version": "foo",
		"os":      "bar",
		"arch":    "baz",
	}))
}
