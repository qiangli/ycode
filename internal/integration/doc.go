// Package integration contains integration tests that run against a live ycode
// instance and its OTEL services. These tests require the "integration" build
// tag and a running server (see Makefile "validate" target).
//
// Run with:
//
//	go test -tags integration -v ./internal/integration/...
package integration
