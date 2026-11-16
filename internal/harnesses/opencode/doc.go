// Package opencode is the OpenCode CLI adapter.
//
// Each session owns one `opencode serve` process, started by OpenSession on
// 127.0.0.1 with an ephemeral port and a random per-session password
// (OPENCODE_SERVER_PASSWORD), and stopped by Close. Tools execute in that
// server. Every client command of the session (`opencode api` control and
// queries, `opencode run` turns) connects to it with `--server <url>` and
// OPENCODE_PASSWORD; a command without the session's server is refused, so the
// user's shared background service is never started, stopped or contacted.
//
// On macOS the server runs under sandbox-exec with a Seatbelt profile that
// denies reading and writing ~/.conduit (rules.DecideSandbox). When the
// sandbox cannot be applied, and on every other platform, the server runs
// without it and protected paths stay unknown with the refusal as reason.
// After readiness OpenSession reads the server's server.info once; its version
// is the harness version only when the answer names exactly the session's
// endpoint and the owned root PID (rules.DecideServerInfo). Otherwise the
// version stays unknown and the session still opens. Protected paths become
// enforced only through evidence recorded live for the exact launch
// (rules.NativeContextEvidence, bound to version, launch hash and platform).
//
// Close stops the server's process group after any run client it terminates;
// execution is reported ended only when that stop is confirmed. A server that
// exits while the session is open loses the session.
package opencode
