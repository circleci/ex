/*
Package runner allows you to run a binary in an acceptance test (scan output for ports,
wait for start).

It is part of our belief that testing binaries that will be shipping into production
with as little modification as is possible is one of the most effective ways of
producing high value tests.

Binaries compiled using the testing/compiler can be used to produce binaries with coverage
instrumentation. In that case setting a CoverageReportPath on a runner for one of those
binaries will help produce a coverage report. Using runner.Stop will send the running
process a INT signal, so the service should respond to that elegantly so that the report
can be flushed to disk.
*/
package runner
