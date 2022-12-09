package runner

import (
	"context"
	"path"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/golden"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/testing/compiler"
)

func TestRunner(t *testing.T) {
	ctx := context.Background()

	binary := ""
	c := compiler.NewParallel(1)
	t.Cleanup(c.Cleanup)

	var err error
	c.Add(compiler.Work{
		Name:   "my-binary",
		Target: ".",
		Source: "./internal/testservice",
		Result: &binary,
	})

	err = c.Run(ctx)
	assert.Assert(t, err)

	t.Run("api_and_admin", func(t *testing.T) {
		r := NewWithDynamicEnv(
			[]string{
				"a=a",
				"b=b",
				"c=c",
			},
			func() []string {
				return []string{
					"d=d",
				}
			},
		)
		t.Cleanup(func() {
			assert.Check(t, r.Stop())
		})

		var res *Result
		t.Run("Start service", func(t *testing.T) {
			var err error
			res, err = r.Run("the-server-name", binary, "e=e")
			assert.Assert(t, err)
		})

		t.Run("Check the right environment was set", func(t *testing.T) {
			c := httpclient.New(httpclient.Config{
				Name:       "the-client-name",
				BaseURL:    res.APIAddr(),
				AcceptType: httpclient.JSON,
				Timeout:    2 * time.Second,
			})

			var env []string
			err = c.Call(ctx, httpclient.NewRequest("GET", "/api/env",
				httpclient.JSONDecoder(&env),
			))
			assert.Check(t, err)
			assert.Check(t, cmp.DeepEqual([]string{"a=a", "b=b", "c=c", "d=d", "e=e"}, env))
		})
	})

	t.Run("admin_only", func(t *testing.T) {
		r := New()
		t.Cleanup(func() {
			assert.Check(t, r.Stop())
		})
		t.Run("start_service", func(t *testing.T) {
			var err error
			_, err = r.Run("", binary, "ADMIN_ONLY=true")
			assert.Assert(t, err)
		})
		t.Run("stop_service", func(t *testing.T) {
			err := r.Stop()
			assert.Assert(t, err)
		})
	})

	t.Run("no_api", func(t *testing.T) {
		r := New()
		t.Cleanup(func() {
			assert.Check(t, r.Stop())
		})
		t.Run("start_service_fail", func(t *testing.T) {
			result, err := r.Start(binary, "ADMIN_ONLY=true")
			assert.Assert(t, err)
			defer func() { _ = result.Stop() }()

			err = result.Ready("not-the-server-name", time.Second)
			assert.Check(t, cmp.ErrorContains(err, "timeout hit"))
		})
	})
}

func TestRunner_Coverage(t *testing.T) {
	ctx := context.Background()

	binary := ""
	c := compiler.NewParallel(1)
	t.Cleanup(c.Cleanup)

	var err error
	c.Add(compiler.Work{
		Name:         "my-binary",
		Target:       ".",
		Source:       "./internal/testservice",
		Result:       &binary,
		WithCoverage: true,
	})

	err = c.Run(ctx)
	assert.Assert(t, err)

	t.Run("api_and_admin", func(t *testing.T) {
		r := New()

		repDir := t.TempDir()
		r.CoverageReportDir(repDir)

		var res *Result
		t.Run("Start service", func(t *testing.T) {
			var err error
			res, err = r.Run("the-server-name", binary)
			assert.Assert(t, err)

			t.Run("Check coverage report", func(t *testing.T) {
				err := res.Stop()
				assert.Assert(t, err)
				repFile := path.Join(repDir, "my-binary") + ".out"
				assert.Check(t, golden.String(string(golden.Get(t, repFile)), "coverage.txt"))
			})
		})
	})
}

func TestGetPort(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		expectPort string
	}{
		{
			"ipv4",
			"server: new-server app.address=127.0.0.1:80 asdasdasdsa",
			"80",
		},
		{
			"ipv6",
			"server: new-server app.address=[::]:80 asdasdasdsa",
			"80",
		},
		{
			"invalid",
			"app.address=:80 asdasdasdsa",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(getPort([]string{tt.line}, "", ""), tt.expectPort))
		})
	}
}
