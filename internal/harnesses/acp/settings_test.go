package acp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func modeOption(current string, values ...string) map[string]any {
	options := make([]any, 0, len(values))
	for _, value := range values {
		options = append(options, map[string]any{"value": value, "name": value})
	}
	return map[string]any{"id": "mode", "name": "Mode", "category": "mode", "type": "select", "currentValue": current, "options": options}
}

// openWithSettings opens a session whose descriptor requires mode=auto.
func openWithSettings(t *testing.T, serve func(*agentConn)) (*fakeLauncher, provider.OpenSessionResult, error) {
	t.Helper()
	launcher := &fakeLauncher{serve: serve}
	descriptor := testDescriptor()
	descriptor.SessionSettings = []ConfigSetting{{ConfigID: "mode", Value: "auto"}}
	adapter := New(descriptor, Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opened, err := adapter.OpenSession(ctx, openRequest(t.TempDir()))
	if err == nil {
		t.Cleanup(func() {
			_, _ = opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
		})
	}
	return launcher, opened, err
}

func settingsAgent(options []any, onSet func(*agentConn, clientFrame)) func(*agentConn) {
	return standardAgent(nil, map[string]any{"sessionId": testSessionID, "configOptions": options}, func(conn *agentConn, frame clientFrame) {
		if frame.Method == "session/set_config_option" {
			onSet(conn, frame)
		}
	})
}

func TestFixtureSessionSettingIsApplied(t *testing.T) {
	failures := make(chan error, 1)
	launcher, opened, err := openWithSettings(t, replayAgent(loadFixture(t, "session_settings.ndjson"), failures))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if _, err := opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseRequireQuiescent}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitReplay(t, failures)
	frames := launcher.framesFor("session/set_config_option")
	if len(frames) != 1 || !strings.Contains(string(frames[0].Params), `"configId":"mode","value":"auto"`) {
		t.Fatalf("set_config_option = %+v", frames)
	}
}

func TestSessionSettingAlreadyCurrentSkipsCall(t *testing.T) {
	launcher, _, err := openWithSettings(t, settingsAgent([]any{modeOption("auto", "auto", "ask")}, func(conn *agentConn, frame clientFrame) {
		conn.reply(frame.ID, map[string]any{})
	}))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if frames := launcher.framesFor("session/set_config_option"); len(frames) != 0 {
		t.Fatalf("current setting was re-sent: %+v", frames)
	}
}

func TestSessionSettingFailuresAreUnsupportedAndReaped(t *testing.T) {
	cases := []struct {
		name    string
		options []any
		onSet   func(*agentConn, clientFrame)
		native  bool
	}{
		{name: "option missing", options: []any{}},
		{name: "value not listed", options: []any{modeOption("ask", "code", "ask")}},
		{
			name: "set rejected", options: []any{modeOption("ask", "auto", "ask")}, native: true,
			onSet: func(conn *agentConn, frame clientFrame) { conn.replyError(frame.ID, -32602, "Invalid params", nil) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			onSet := tc.onSet
			if onSet == nil {
				onSet = func(conn *agentConn, frame clientFrame) { conn.reply(frame.ID, map[string]any{}) }
			}
			launcher, _, err := openWithSettings(t, settingsAgent(tc.options, onSet))
			requireCode(t, err, provider.ErrorCapabilityUnsupported)
			var nativeErr *rpcError
			if errors.As(err, &nativeErr) != tc.native {
				t.Fatalf("native cause preserved = %v, want %v (%v)", !tc.native, tc.native, err)
			}
			if tc.native && len(launcher.framesFor("session/set_config_option")) != 1 {
				t.Fatal("rejected set was not sent")
			}
			if !tc.native && len(launcher.framesFor("session/set_config_option")) != 0 {
				t.Fatal("unsupported setting was sent")
			}
			if !launcher.process(t).reaped() {
				t.Fatal("failed open left the agent running")
			}
		})
	}
}
