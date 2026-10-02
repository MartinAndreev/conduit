// Package provider defines the consumer-owned contract between Conduit sessions
// and agent-harness adapters.
//
// A Provider adapts a harness such as Codex App Server or OpenCode. It is not an
// inference API provider. The package models transport observations and control
// requests; it does not implement orchestration, persistence, retries, result
// acceptance, or completion policy.
package provider
