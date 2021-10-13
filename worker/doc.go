/*
Package worker runs a service worker loop with observability and back-off for no work found.

It is used by various `ex` packages internally, and can be used for any regular work your
service might need to do, such as consuming queue-like data sources.
*/
package worker
