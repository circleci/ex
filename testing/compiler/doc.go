/*
Package compiler helps efficiently compile and cleanup your services in acceptance tests.

This is a small wrapper around the general releases/compiler. The binary created from this package
will always be stored in a temporary folder.

To instrument a binary with coverage, the main entry point package must include a TestRunMain
func (or similar )in a test file as per the example in internal/cmd/main_test.go. The work added
to the compiler must set the WithCoverage flag to true.

For the resultant binary to produce a coverage report it needs to be called with these parameters:
"-test.run", "^TestRunMain$", "-test.coverprofile", "your_coverage_name.out" and the service needs
to be able to exit cleanly to flush the report.
The helper function in testing/runner can make this easier.
*/
package compiler
