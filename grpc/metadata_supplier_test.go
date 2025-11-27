package grpc

import (
	"testing"

	"google.golang.org/grpc/metadata"
	"gotest.tools/v3/assert"
)

func TestMetadataSupplier(t *testing.T) {
	md := metadata.New(map[string]string{
		"k1": "v1",
	})
	ms := &metadataSupplier{&md}

	v1 := ms.Get("k1")
	assert.Equal(t, v1, "v1")

	ms.Set("k2", "v2")

	v1 = ms.Get("k1")
	v2 := ms.Get("k2")
	assert.Equal(t, v1, "v1")
	assert.Equal(t, v2, "v2")
}
