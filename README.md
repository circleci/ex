# Go service building blocks
This repository contains a collection of packages designed to aid building
reliable, observable, zero-downtime services using Go.

## What is in here?
- `config/o11y` Wiring code for the `o11y` package.
- `config/secret` Don't store your secrets in a string that can be accidentally logged.
  Use a `secret.String` instead.
- `datadog` A basic Datadog API client for retrieving metrics back from Datadog.
- `db` Common patterns using when talking to an RDBMS. Only supports PostgreSQL at present.
- `httpclient` A simple HTTP client that adds observability and resilience to the standard
  Go HTTP client.
- `httpserver` Starting and stopping the standard Go http server cleanly.
- `httpserver/ginrouter` A common base for configuring a Gin router instance.
- `httpserver/healthcheck` A healthcheck HTTP server that can accept all the checks from a `system`.
- `o11y` Observability that is currently backed by Honeycomb. It also supports outputting
  trace data as JSON and plain or colored text output.
- `o11y/honeycomb` The honeycomb-backed implementation of `o11y`.
- `o11y/wrappers/o11ygin` `o11y` middleware for the Gin router.
- `o11y/wrappers/o11ynethttp` `o11y` middleware for the standard Go HTTP server.
- `rabbit` Experimental RabbitMQ publishing client.
- `redis` Wiring and observability for Redis.
- `system` Manage the startup, running, metrics and shutdown of a Go service.
- `termination` A handler to aid signal based service termination. (Used internally by
  the `system` package).
- `testing/compiler` At CircleCI we aim to acceptance test whole compiled binaries. This
  package lets us do that, while capturing test coverage for the binary.
- `testing/dbfixture` Get a resettable unique database for each test.
- `testing/download` Download releases of binaries for using in end to end service testing.
- `testing/fakemetrics` A fake recording `o11y` metrics implementation.
- `testing/httprecorder` Record HTTP requests inside an HTTP server, and search them.
- `testing/kongtest` If you are using [kong](https://github.com/alecthomas/kong) for your
  CLI parsing, this helps in writing golden tests for the CLI definition.
- `testing/rabbitfixture` Get an isolated RabbitMQ VHost for your tests, so they don't interfere.
- `testing/redisfixture` Get an isolated Redis DB for your tests, so they don't interfere.
- `testing/releases` Helper to determine which binaries to download for end to end tests.
- `testing/runner` Run a binary in an acceptance test (scan output for ports, wait for start). 
- `testing/testcontext` Setup a background context that includes `o11y`.
- `worker` Run a service worker loop with observability and back-off for no work found.

## Who is this for?
These packages are intended to be used internally at CircleCI. While this code is licenced
permissively with an MIT licence, this is not a true "open source" project, in that there is
no active community using it. 

## No guaranteed API stability
While we do not intentionally aim to break compatibility, we make no promises that we will
maintain a stable API if there are good reasons to break it.
