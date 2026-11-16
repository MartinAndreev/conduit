package rules

import (
	"strings"
	"testing"
)

func TestDecideServerReady(t *testing.T) {
	tests := []struct {
		line string
		want ServerReadyKind
		url  string
	}{
		{"server listening on http://127.0.0.1:4096", ServerReadyListening, "http://127.0.0.1:4096"},
		{"server listening on http://127.0.0.1:1", ServerReadyListening, "http://127.0.0.1:1"},
		{"server listening on http://127.0.0.1:65535", ServerReadyListening, "http://127.0.0.1:65535"},
		{"", ServerReadyOtherOutput, ""},
		{"INFO starting", ServerReadyOtherOutput, ""},
		{" server listening on http://127.0.0.1:4096", ServerReadyOtherOutput, ""},
		{"server listening on http://0.0.0.0:4096", ServerReadyAddressRejected, ""},
		{"server listening on http://localhost:4096", ServerReadyAddressRejected, ""},
		{"server listening on https://127.0.0.1:4096", ServerReadyAddressRejected, ""},
		{"server listening on http://[::1]:4096", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:0", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:65536", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:04096", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:+4096", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:4096/", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:4096 ", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:4096\r", ServerReadyAddressRejected, ""},
		{"server listening on http://127.0.0.1:123456", ServerReadyAddressRejected, ""},
		{"server listening on http://user@127.0.0.1:4096", ServerReadyAddressRejected, ""},
		{"server password s3cr3t", ServerReadyPasswordGenerated, ""},
	}
	for _, tt := range tests {
		decision := DecideServerReady(ServerReadyFacts{Line: tt.line})
		if decision.Kind != tt.want || decision.URL != tt.url || decision.Reason == "" {
			t.Fatalf("DecideServerReady(%q) = %#v, want %s %q", tt.line, decision, tt.want, tt.url)
		}
		if strings.Contains(decision.Reason, "s3cr3t") {
			t.Fatal("reason quotes the server output")
		}
	}
}

func TestDecideServerStop(t *testing.T) {
	stopped := ServerStopFacts{CleanupComplete: true, RootReaped: true, TreeKillSupported: true}
	with := func(change func(*ServerStopFacts)) ServerStopFacts {
		facts := stopped
		change(&facts)
		return facts
	}
	tests := []struct {
		name  string
		facts ServerStopFacts
		want  ServerStopKind
	}{
		{"confirmed", stopped, ServerStopConfirmed},
		{"kill failed", with(func(f *ServerStopFacts) { f.TreeKillFailed = true }), ServerStopKillFailed},
		{"kill failure precedes pending", ServerStopFacts{TreeKillFailed: true}, ServerStopKillFailed},
		{"cleanup pending", with(func(f *ServerStopFacts) { f.CleanupComplete = false }), ServerStopPending},
		{"root not reaped", with(func(f *ServerStopFacts) { f.RootReaped = false }), ServerStopPending},
		{"tree kill unsupported", with(func(f *ServerStopFacts) { f.TreeKillSupported = false }), ServerStopTreeUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DecideServerStop(tt.facts)
			if decision.Kind != tt.want || decision.Reason == "" || decision.Confirmed() != (tt.want == ServerStopConfirmed) {
				t.Fatalf("decision = %#v, want %s", decision, tt.want)
			}
		})
	}
}

func TestDecideServerExit(t *testing.T) {
	if d := DecideServerExit(ServerExitFacts{Closed: true}); d.Kind != ServerExitExpected || d.Reason == "" {
		t.Fatalf("closed = %#v", d)
	}
	if d := DecideServerExit(ServerExitFacts{}); d.Kind != ServerExitSessionLost || d.Reason == "" {
		t.Fatalf("open = %#v", d)
	}
}

func TestDecideServerInfo(t *testing.T) {
	good := ServerInfoFacts{Version: "2.0.22", URLs: []string{"http://127.0.0.1:4096"}, PID: 42, ExpectedURL: "http://127.0.0.1:4096", ExpectedPID: 42}
	with := func(change func(*ServerInfoFacts)) ServerInfoFacts {
		facts := good
		facts.URLs = append([]string(nil), good.URLs...)
		change(&facts)
		return facts
	}
	tests := []struct {
		name  string
		facts ServerInfoFacts
		want  ServerInfoKind
	}{
		{"accepted", good, ServerInfoAccepted},
		{"owned pid unknown proves nothing", with(func(f *ServerInfoFacts) { f.ExpectedPID, f.PID = 0, 0 }), ServerInfoPIDMismatch},
		{"pid mismatch precedes invalid version", with(func(f *ServerInfoFacts) { f.PID, f.Version = 43, "x" }), ServerInfoPIDMismatch},
		{"url mismatch precedes pid mismatch", with(func(f *ServerInfoFacts) { f.PID, f.URLs = 43, nil }), ServerInfoURLMismatch},
		{"query failed", with(func(f *ServerInfoFacts) { f.QueryFailed = true }), ServerInfoQueryFailed},
		{"no urls", with(func(f *ServerInfoFacts) { f.URLs = nil }), ServerInfoURLMismatch},
		{"other url", with(func(f *ServerInfoFacts) { f.URLs = []string{"http://127.0.0.1:4097"} }), ServerInfoURLMismatch},
		{"extra url", with(func(f *ServerInfoFacts) { f.URLs = append(f.URLs, "http://127.0.0.1:4097") }), ServerInfoURLMismatch},
		{"url not exact", with(func(f *ServerInfoFacts) { f.URLs = []string{"http://127.0.0.1:4096/"} }), ServerInfoURLMismatch},
		{"no expected url", with(func(f *ServerInfoFacts) { f.ExpectedURL, f.URLs = "", []string{""} }), ServerInfoURLMismatch},
		{"other pid", with(func(f *ServerInfoFacts) { f.PID = 43 }), ServerInfoPIDMismatch},
		{"missing pid", with(func(f *ServerInfoFacts) { f.PID = 0 }), ServerInfoPIDMismatch},
		{"empty version", with(func(f *ServerInfoFacts) { f.Version = "" }), ServerInfoVersionInvalid},
		{"v prefix", with(func(f *ServerInfoFacts) { f.Version = "v2.0.22" }), ServerInfoVersionInvalid},
		{"pre-release", with(func(f *ServerInfoFacts) { f.Version = "2.0.22-beta.1" }), ServerInfoVersionInvalid},
		{"two parts", with(func(f *ServerInfoFacts) { f.Version = "2.0" }), ServerInfoVersionInvalid},
		{"four parts", with(func(f *ServerInfoFacts) { f.Version = "2.0.22.1" }), ServerInfoVersionInvalid},
		{"empty part", with(func(f *ServerInfoFacts) { f.Version = "2..22" }), ServerInfoVersionInvalid},
		{"space", with(func(f *ServerInfoFacts) { f.Version = "2.0.22 " }), ServerInfoVersionInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DecideServerInfo(tt.facts)
			if d.Kind != tt.want || d.Reason == "" || (d.Version != "") != (tt.want == ServerInfoAccepted) {
				t.Fatalf("decision = %#v, want %s", d, tt.want)
			}
			if d.ProvesOwnServer() != (tt.want == ServerInfoAccepted || tt.want == ServerInfoVersionInvalid) {
				t.Fatalf("ProvesOwnServer = %t for %s", d.ProvesOwnServer(), d.Kind)
			}
			if tt.want == ServerInfoAccepted && d.Version != tt.facts.Version {
				t.Fatalf("version = %q", d.Version)
			}
		})
	}
}
