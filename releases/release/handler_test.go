package release_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/releases/release"
	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/httprecorder/ginrecorder"
	"github.com/circleci/ex/testing/testcontext"
)

func TestHandler(t *testing.T) {
	ctx := testcontext.Background()

	t.Run("Test success", func(t *testing.T) {
		fix := startAPI(ctx, t)

		t.Run("Can get a release", func(t *testing.T) {
			agent, err := fix.Download(ctx, release.Requirements{
				Platform: "linux",
				Arch:     "amd64",
			})

			assert.NilError(t, err)
			assert.Check(t, cmp.DeepEqual(agent, &release.Release{
				URL:      fix.S3URL + "/1.1.1-abcdef01/linux/amd64/circleci-agent",
				Checksum: "4a62f09b64873a20386cdbfaca87cc10d8352fab014ef0018f1abcce08a3d027",
				Version:  "1.1.1-abcdef01",
			}))
		})
	})

	t.Run("Test for unknown arch", func(t *testing.T) {
		fix := startAPI(ctx, t)

		t.Run("Can get a release", func(t *testing.T) {
			_, err := fix.Download(ctx, release.Requirements{
				Platform: "linux",
				Arch:     "enemy",
			})

			assert.Check(t, cmp.ErrorContains(err, "404 (Not Found)"))
		})
	})

	t.Run("Test invalid requests", func(t *testing.T) {
		fix := startAPI(ctx, t)

		t.Run("No platform", func(t *testing.T) {
			_, err := fix.Download(ctx, release.Requirements{
				Arch: "enemy",
			})

			assert.Check(t, cmp.ErrorContains(err, "400 (Bad Request)"))
		})
		t.Run("No arch", func(t *testing.T) {
			_, err := fix.Download(ctx, release.Requirements{
				Platform: "linux",
			})

			assert.Check(t, cmp.ErrorContains(err, "400 (Bad Request)"))
		})
	})
}

func (f *fixture) Download(ctx context.Context, requirements release.Requirements) (*release.Release, error) {
	var resp release.Release
	err := f.Client.Call(ctx, httpclient.NewRequest("GET", "/downloads",
		httpclient.Body(requirements),
		httpclient.JSONDecoder(&resp),
		httpclient.NoRetry(),
	))
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

type fixture struct {
	Client *httpclient.Client
	S3URL  string
}

func startAPI(ctx context.Context, t *testing.T) fixture {
	s3srv := httptest.NewServer(newFakeS3("", httprecorder.New()))
	t.Cleanup(s3srv.Close)

	agentList, err := release.NewList(ctx, "agent", "", s3srv.URL)
	assert.Assert(t, err)

	r := ginrouter.Default(ctx, "fake-downloads")
	r.GET("downloads", release.Handler(agentList))

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return fixture{
		Client: httpclient.New(httpclient.Config{
			Name:    "test-client",
			BaseURL: srv.URL,
		}),

		S3URL: s3srv.URL,
	}
}

func newFakeS3(checksums string, recorder *httprecorder.RequestRecorder) http.Handler {
	ctx := testcontext.Background()
	r := ginrouter.Default(ctx, "fake-s3")
	r.Use(ginrecorder.Middleware(ctx, recorder))

	r.GET("release.txt", func(c *gin.Context) {
		c.String(http.StatusOK, "1.1.1-abcdef01\n")
	})

	r.GET(":version/checksums.txt", func(c *gin.Context) {
		version := c.Param("version")

		switch version {
		case "1.1.1-abcdef01", "2.2.2-fedcba12":
			// known checksum version
			if checksums == "" {
				checksums = `2b01eb92dfc89274c804b6b90423e0fc65f97af3f5e0ceb676657826886fabb2 *darwin/amd64/circleci-agent
4a62f09b64873a20386cdbfaca87cc10d8352fab014ef0018f1abcce08a3d027 *linux/amd64/circleci-agent
11h32jhg123g123hg12j3h1g2j3hg12j3hg12jh3gj12h3gjh12g3jh1g2j3hg12 *linux/arm64be/circleci-agent
0293e95dbf217ead2de55c0a7a0f15e6641b41cf8a99a64f5d6c8fcc7f670bb3 *windows/amd64/circleci-agent.exe`
			}
			c.String(http.StatusOK, checksums)
		}
	})

	r.GET("/:version/:os/:arch/circleci-agent", func(c *gin.Context) {
		version := c.Param("version")
		os := c.Param("os")
		arch := c.Param("arch")
		switch {
		case version != "" && os != "" && arch != "":
			c.String(http.StatusOK, "this is a fake agent")
		default:
			c.Status(http.StatusNotFound)
		}
	})
	return r
}
