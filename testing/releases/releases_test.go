package releases

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestReleases_Version(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, "1.2.3-abc\n")
	}))

	rel := New(srv.URL)
	ver, err := rel.Version(ctx)
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, "1.2.3-abc"))
}

const checksum = `
38b7ff561f90f30cb26667512211262b683ab7cb82987f5abe3b572e98b14f7d *darwin/amd64/internal
94ff99a5c0fd73739529562e988164c62ce5eb5b18ebe15e6aecfcc8e393fb05 *darwin/amd64/public
f78e202de408a5162b6e888d32e9bcf939271bab7bd4c5751fd931eb416dd1c3 *darwin/amd64/receiver
ed5eca3dabd1e7b40824893fe2f3314de0eb276b8d8c8585c0e38a0bf9f32137 *darwin/arm64/internal
58c7c14d5d0e9e07035ac7643386706a264c10eb3ee9f26bb459a04289c5effe *darwin/arm64/public
ef5144178d6e6ca32ac142a6e3bbe2506e92c4be9e1a7771d1a96e68eaac43e4 *darwin/arm64/receiver
c3ca214a6c50ed3031a380ecaf2518da0c44840fe3b1a99f60ea45301b2f5564 *linux/amd64/internal
e1dd9c1607892abc43a976b874814523f59e9f27ab7154d2b05339978bb6a895 *linux/amd64/public
d026f12336c64ae9ee57ca848f1485f5931f1019a46ecb8a088833010f8f7a6d *linux/amd64/receiver
f5890ec97fc047677603ccffb815e59df1e39a4219868b66d92318ebec9e50be *linux/arm/internal
d7045e25ab522bdc057b6626e7a566409b369d65836cc1cd4e16038c2f4cb573 *linux/arm/public
8ae6ba30a84adbb305a2d0f522de87c2b73625168c940fe8eadde91804e92a22 *linux/arm/receiver
42e6fc28ad89b0e5cd866c3056224204cac27c4a85b6b88be3de7d342041ce7d *linux/arm64/internal
86fff2ac7fabbd936be4f4a069e0a854492660d6214541a1ae8e2d059b541b66 *linux/arm64/public
66c5bb6f834b6b1016de9f9ee99c7e4776d819d6c7155d7a226e201735d91bcb *linux/arm64/receiver
dd6c2a3230952f4cec6190ca030372a3a1a6d04d6d483d81bb06c506032b0eba *windows/amd64/internal.exe
2986031acbd08d930c06131e64f68fe9c32fdaac32d028e53f178afc2dd4889b *windows/amd64/public.exe
3064327451adf87ed823410e23837a2843f1cb823480be3909db91ef85c79141 *windows/amd64/receiver.exe
`

func TestReleases_ResolveURL(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.2.3-abc/checksums.txt" {
			t.Log(r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, checksum)
	}))

	rel := New(srv.URL)
	ver, err := rel.ResolveURL(ctx, Requirements{
		Version: "1.2.3-abc",
		OS:      "linux",
		Arch:    "amd64",
	})
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, srv.URL+"/1.2.3-abc/linux/amd64/internal"))
	ver, err = rel.ResolveURL(ctx, Requirements{
		Version: "1.2.3-abc",
		OS:      "linux",
		Arch:    "arm",
	})
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, srv.URL+"/1.2.3-abc/linux/arm/internal"))
	ver, err = rel.ResolveURL(ctx, Requirements{
		Version: "1.2.3-abc",
		OS:      "linux",
		Arch:    "arm64",
	})
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, srv.URL+"/1.2.3-abc/linux/arm64/internal"))
}

func TestReleases_ResolveURLs(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.2.3-abc/checksums.txt" {
			t.Log(r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, checksum)
	}))

	rel := New(srv.URL)

	tests := []Requirements{
		{
			Version: "1.2.3-abc",
			OS:      "linux",
			Arch:    "amd64",
		},
		{
			Version: "1.2.3-abc",
			OS:      "linux",
			Arch:    "arm64",
		},
		{
			Version: "1.2.3-abc",
			OS:      "windows",
			Arch:    "amd64",
		},
		{
			Version: "1.2.3-abc",
			OS:      "darwin",
			Arch:    "amd64",
		},
		{
			Version: "1.2.3-abc",
			OS:      "darwin",
			Arch:    "arm64",
		},
	}
	for _, tt := range tests {
		baseURL := fmt.Sprintf("%s/%s/%s/%s/", srv.URL, tt.Version, tt.OS, tt.Arch)
		t.Run(baseURL, func(t *testing.T) {
			urls, err := rel.ResolveURLs(ctx, tt)
			assert.Assert(t, err)

			expect := map[string]string{
				"internal": baseURL + "internal",
				"public":   baseURL + "public",
				"receiver": baseURL + "receiver",
			}
			if tt.OS == "windows" {
				expect = map[string]string{
					"internal": baseURL + "internal.exe",
					"public":   baseURL + "public.exe",
					"receiver": baseURL + "receiver.exe",
				}
			}
			assert.Check(t, cmp.DeepEqual(urls, expect))
		})

	}
}
