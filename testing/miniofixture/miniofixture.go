// Package miniofixture is deprecated: use testing/s3fixture instead.
//
// This package is a thin compatibility wrapper over s3fixture.
// It will be removed in a future version once downstream repos have migrated.
package miniofixture

import (
	"context"
	"testing"

	"github.com/circleci/ex/testing/s3fixture"
)

// Fixture is an alias for s3fixture.Fixture.
//
// Deprecated: use s3fixture.Fixture instead.
type Fixture = s3fixture.Fixture

// Default sets up and returns the default fixture.
//
// Deprecated: use s3fixture.Default instead.
func Default(ctx context.Context, t testing.TB) *Fixture {
	return s3fixture.Default(ctx, t)
}

// Setup will take the given fixture adding default values as needed and update the fields in the fixture
// with whatever values were used.
//
// Deprecated: use s3fixture.Setup instead.
func Setup(ctx context.Context, t testing.TB, fix *Fixture) {
	s3fixture.Setup(ctx, t, fix)
}

// BucketName generates a random bucket name scoped to the test.
//
// Deprecated: use s3fixture.BucketName instead.
func BucketName(t testing.TB) string {
	return s3fixture.BucketName(t)
}
