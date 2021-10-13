/*
Package system manages the startup, running, metrics and shutdown of a Go service.

Most, if not all, services need to run a bunch of things in the background (such as
HTTP servers, healthchecks, metrics and worker loops). They also need to shutdown
cleanly when told to. Particularly for services offering REST APIs in Kubernetes, they
should also wait "little time" before shutting down, in order to avoid disconnecting
active users.

This package rolls all this up in an easy to consume form.

See the example project main func for a full canonical example of its usage.
*/
package system
