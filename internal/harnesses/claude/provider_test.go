package claude

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestLaunchEnvironmentStripsCredentialsAndSessionVariables(t *testing.T) {
	parent := []string{
		"PATH=/bin", "HOME=/home/user",
		"ANTHROPIC_API_KEY=key", "ANTHROPIC_AUTH_TOKEN=token", "ANTHROPIC_BASE_URL=https://example.test",
		"CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_SSE_PORT=1234", "CLAUDE_CODE_OAUTH_TOKEN=oauth",
		"CLAUDE_EFFORT=high", "CLAUDE_PID=7", "CLAUDE_AGENT_SDK_VERSION=1", "CLAUDE_CONFIG_DIR=/cfg",
		"CLAUDE_CODE_ENTRYPOINT=duplicate", "MALFORMED",
	}
	got := launchEnvironment(parent)
	want := []string{"PATH=/bin", "HOME=/home/user", "ANTHROPIC_BASE_URL=https://example.test", "CLAUDE_CONFIG_DIR=/cfg", "MALFORMED"}
	if !slices.Equal(got, want) {
		t.Fatalf("launch environment = %q, want %q", got, want)
	}
	if env := launchEnvironment(nil); env == nil || len(env) != 0 {
		t.Fatalf("empty parent environment = %#v, want empty non-nil", env)
	}
}

func TestLaunchArguments(t *testing.T) {
	base := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages", "--replay-user-messages", "--session-id", "sid", "--model", "m",
		"--permission-mode", "acceptEdits",
	}
	tail := []string{"--settings", `{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true}}`, "--disallowedTools", "WebFetch,WebSearch"}
	if got := launchArguments("sid", "m", nil); !slices.Equal(got, append(slices.Clone(base), tail...)) {
		t.Fatalf("arguments = %q", got)
	}
	withSchema := append(append(slices.Clone(base), "--json-schema", `{"type":"object"}`), tail...)
	if got := launchArguments("sid", "m", json.RawMessage(`{"type":"object"}`)); !slices.Equal(got, withSchema) {
		t.Fatalf("schema arguments = %q", got)
	}
}

func TestNewUUIDIsVersion4(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := newUUID()
	if !pattern.MatchString(first) || first == second {
		t.Fatalf("uuids %q %q", first, second)
	}
}

func TestOpenSessionLaunchesAndInitializes(t *testing.T) {
	fake := newFakeClaude(t)
	request := openRequest(t, noneContract)
	opened, err := New(testConfig(fake)).OpenSession(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
	spec, _ := fake.launchSpec()
	if spec.Path != "/test/claude" || spec.Dir != request.Workspace.Path {
		t.Fatalf("launch spec = %#v", spec)
	}
	if want := launchArguments(fake.nativeSession(), "claude-test-model", nil); !slices.Equal(spec.Args, want) {
		t.Fatalf("args = %q, want %q", spec.Args, want)
	}
	if !spec.ReplaceEnv || !slices.Equal(spec.Env, []string{"PATH=/bin"}) {
		t.Fatalf("env = %q replace=%v", spec.Env, spec.ReplaceEnv)
	}
	if subtypes := fake.controlSubtypes(); len(subtypes) != 1 || subtypes[0] != "initialize" {
		t.Fatalf("control requests = %q", subtypes)
	}
	if opened.Contract.Actual != provider.SchemaNone {
		t.Fatalf("contract = %#v", opened.Contract)
	}
	capabilities := opened.Session.Capabilities()
	if capabilities.HarnessVersion != "2.1.316" || capabilities.ProviderName != "claude" {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	for _, item := range capabilities.Items {
		switch item.Capability {
		case provider.CapabilityResultNativeSchema, provider.CapabilitySteerActiveTurn, provider.CapabilityEventCommand:
			if item.Support != provider.SupportUnknown {
				t.Fatalf("%s support = %s, want unknown", item.Capability, item.Support)
			}
		}
	}
}

// Startup events are buffered before native work so a late subscriber sees them.
func TestLateSubscriberReceivesBufferedStartupEvents(t *testing.T) {
	fake := newFakeClaude(t)
	opened, err := New(testConfig(fake)).OpenSession(context.Background(), openRequest(t, noneContract))
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
	time.Sleep(20 * time.Millisecond)
	stream, err := opened.Session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := receiveUntilQuiet(stream, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	states := bodiesOf[provider.SessionStateEvent](events)
	if len(states) != 2 || states[0].Current != provider.SessionOpening || states[1].Current != provider.SessionOpen || states[1].NativeStatus != "idle" {
		t.Fatalf("startup states = %#v", states)
	}
	if events[0].Envelope.AdapterSeq != 1 || !events[0].Envelope.Native.Session.Present {
		t.Fatalf("first envelope = %#v", events[0].Envelope)
	}
}

func TestOpenSessionRefusesMissingLoginAndReapsProcess(t *testing.T) {
	fake := newFakeClaude(t)
	fake.initialize = readFixture(t, "initialize_unauthenticated.json")
	_, err := New(testConfig(fake)).OpenSession(context.Background(), openRequest(t, noneContract))
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorAuthFailed || providerErr.Operation != provider.OperationOpenSession || providerErr.Retry != provider.RetryNever {
		t.Fatalf("err = %v, want auth_failed", err)
	}
	proc := fake.process()
	// The cleanup owner closes stdin and kills the group concurrently, so the
	// fake may exit before the kill; the group kill is still requested.
	if proc.killCalls.Load() == 0 || !proc.exited() {
		t.Fatalf("owned process kill requests=%d exited=%v", proc.killCalls.Load(), proc.exited())
	}
	deadline := time.Now().Add(time.Second)
	for !proc.waited.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !proc.waited.Load() {
		t.Fatal("owned process was not reaped")
	}
}

func TestOpenSessionValidatesBeforeLaunch(t *testing.T) {
	fake := newFakeClaude(t)
	adapter := New(testConfig(fake))
	readOnly := openRequest(t, noneContract)
	readOnly.Workspace.Writable = false
	ask := openRequest(t, noneContract)
	ask.PermissionMode = provider.PermissionAsk
	noModel := openRequest(t, noneContract)
	noModel.Model.ModelID = ""
	nativeOnly := openRequest(t, provider.ResultContractRef{Requested: provider.SchemaNative})
	for name, request := range map[string]provider.OpenSessionRequest{"read-only": readOnly, "ask": ask, "no model": noModel, "native without schema": nativeOnly} {
		if _, err := adapter.OpenSession(context.Background(), request); err == nil {
			t.Fatalf("%s: open succeeded", name)
		}
	}
	if _, starts := fake.launchSpec(); starts != 0 {
		t.Fatalf("validation failure launched %d processes", starts)
	}
}

func TestOpenSessionNegotiatesNativeSchema(t *testing.T) {
	fake := newFakeClaude(t)
	opened, err := New(testConfig(fake)).OpenSession(context.Background(), openRequest(t, schemaContract))
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
	spec, _ := fake.launchSpec()
	args := spec.Args
	index := slices.Index(args, "--json-schema")
	if opened.Contract.Actual != provider.SchemaNative || index < 0 || args[index+1] != `{"type":"object"}` {
		t.Fatalf("contract %#v args %q", opened.Contract, args)
	}
}

func TestInspectDoesNotLaunch(t *testing.T) {
	fake := newFakeClaude(t)
	descriptor, err := New(testConfig(fake)).Inspect(context.Background(), provider.InspectRequest{Model: testModel})
	if _, starts := fake.launchSpec(); err != nil || descriptor.ProviderName != "claude" || starts != 0 {
		t.Fatalf("descriptor %#v err %v starts %d", descriptor, err, starts)
	}
}
