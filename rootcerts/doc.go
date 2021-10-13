/*
Package rootcerts exists to support creating Docker images `FROM scratch`.

For a Go binary to be able to run in a `FROM scratch` image, it needs a few things:
1. It must be compiled with `CGO_ENABLED=0`.
2. It must have access to timezone data (if it handles time data).
3. It must have access to a CA trust store.

The first two are easy, and natively supported by the Go runtime, but third is not.

This package provides a vendored set of root certificates downloaded from Mozilla, embedded
into Go source files. CI will check if this list is up-to-date, and fail the lint job if
it is not.

For most use-cases, calling rootcerts.UpdateDefaultTransport will be all you need to do
from consuming code (e.g. using Go's HTTP client).

Some systems (like the Go AWS SDK) require passing the results of rootcerts.DERReader to setup
the trust store there.
*/
package rootcerts
