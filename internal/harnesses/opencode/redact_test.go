package opencode

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestRedactJSON(t *testing.T) {
	const secret = "abc"
	tests := []struct{ raw, want string }{
		{`{"a":"x"}`, `{"a":"x"}`},
		{`{"b":1,"a":"abc"}`, `{"a":"` + redactionMarker + `","b":1}`},
		{`{"a":"\u0061bc"}`, `{"a":"` + redactionMarker + `"}`},
		{`["\u0061bc",{"abc":[true,null,1.50]}]`, `["` + redactionMarker + `",{"` + redactionMarker + `":[true,null,1.50]}]`},
		{`"abc"`, `"` + redactionMarker + `"`},
		{`not json abc`, `not json ` + redactionMarker},
		{``, ``},
	}
	for _, tt := range tests {
		if got := string(redactJSON([]byte(tt.raw), secret)); got != tt.want {
			t.Fatalf("redactJSON(%s) = %s, want %s", tt.raw, got, tt.want)
		}
	}
}

func TestRedactSecret(t *testing.T) {
	const secret = "0123abcd"
	tests := []struct{ text, secret, want string }{
		{"OPENCODE_SERVER_PASSWORD=0123abcd\n", secret, "OPENCODE_SERVER_PASSWORD=" + redactionMarker + "\n"},
		{"0123abcd0123abcd", secret, redactionMarker + redactionMarker},
		{"a 0123abcd b 0123abcd", secret, "a " + redactionMarker + " b " + redactionMarker},
		{"nothing here", secret, "nothing here"},
		{"partial 0123abc", secret, "partial 0123abc"},
		{"anything", "", "anything"},
		{"", secret, ""},
	}
	for _, tt := range tests {
		if got := redactSecret(tt.text, tt.secret); got != tt.want {
			t.Fatalf("redactSecret(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// Tools inherit OPENCODE_SERVER_PASSWORD, so the password can appear in any
// native text. No emitted event may carry it.
func TestSessionPasswordIsNeverEmitted(t *testing.T) {
	pw := testServerPassword
	lines := []string{
		`{"type":"text","sessionID":"native-session","part":{"id":"t1","text":"the password is ` + pw + `"}}`,
		`{"type":"tool_use","sessionID":"native-session","part":{"id":"b1","tool":"bash","state":{"status":"completed","input":{"command":"echo ` + pw + `; env"},"output":"PATH=/bin\nOPENCODE_SERVER_PASSWORD=` + pw + `\n","metadata":{"exit":0}}}}`,
		`{"type":"tool_use","sessionID":"native-session","part":{"id":"b2","tool":"bash","state":{"status":"error","input":{"command":"false"},"error":"failed near ` + pw + `"}}}`,
		`{"type":"tool_use","sessionID":"native-session","part":{"id":"r1","tool":"read","state":{"status":"completed","input":{"filePath":"/tmp/` + pw + `"},"output":"x"}}}`,
		`{"type":"text","sessionID":"native-session","part":{"id":"t2","text":"escaped \u0074` + pw[1:] + `"}}`,
		`{"type":"` + pw + `","sessionID":"native-session"}`,
		`{"type":"tool_use","sessionID":"native-session","part":{"id":"b3","tool":"bash","state":{"status":"completed","input":{"command":"echo \u0074` + pw[1:] + `","nested":["\u0074` + pw[1:] + `"]},"output":"ok","metadata":{"exit":0}}}}`,
		`{"type":"tool_use","sessionID":"native-session","part":{"id":"\u0074` + pw[1:] + `","tool":"\u0074` + pw[1:] + `","state":{"status":"completed","input":{"\u0074` + pw[1:] + `":"key"},"output":"ok","error":{"message":"\u0074` + pw[1:] + `"}}}}`,
		`{"type":"text","sessionID":"native-session","part":{"id":"\u0074` + pw[1:] + `","text":"id leak"}}`,
	}
	launcher := &fakeLauncher{output: strings.Join(lines, "\n") + "\n"}
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: launcher}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := opened.Session.Events(context.Background(), provider.EventCursor{})
	request := openRequest()
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract}); err != nil {
		t.Fatal(err)
	}
	markers := 0
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatal("run end not observed")
		}
		text := fmt.Sprintf("%#v %#v", event.Envelope, event.Body())
		if strings.Contains(text, pw) {
			t.Fatalf("event carries the password: %T", event.Body())
		}
		markers += strings.Count(text, redactionMarker)
		if body, ok := event.Body().(provider.SessionStateEvent); ok && body.Current == provider.SessionIdleObserved {
			break
		}
	}
	if markers == 0 {
		t.Fatal("no redaction marker emitted")
	}
}
