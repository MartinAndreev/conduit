package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// fakeClaude is an in-process scripted `claude -p` used by every adapter test.
// It answers control requests and hands each user message to onUser, which
// writes native stdout lines. No real claude, model or network is involved.
type fakeClaude struct {
	t          *testing.T
	initialize string
	// onUser runs on the fake's stdin goroutine for every user message.
	onUser func(f *fakeClaude, message map[string]any)
	// exitOnStdinClose makes the fake exit when the adapter closes stdin.
	exitOnStdinClose bool

	mu       sync.Mutex
	spec     processruntime.Spec
	starts   int
	received []map[string]any
	proc     *fakeProcess
	session  string
}

func newFakeClaude(t *testing.T) *fakeClaude {
	return &fakeClaude{t: t, initialize: readFixture(t, "initialize_ok.json"), exitOnStdinClose: true}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func (f *fakeClaude) Start(spec processruntime.Spec) (processruntime.Process, error) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	proc := &fakeProcess{
		stdin: clientInput, stdout: clientOutput, stderr: stderrReader,
		serverInput: serverInput, serverOutput: serverOutput, stderrWriter: stderrWriter,
		done: make(chan struct{}),
	}
	_ = lockscope.WithMutexLock(&f.mu, func() error {
		f.spec = spec
		f.starts++
		f.proc = proc
		for index, arg := range spec.Args {
			if arg == "--session-id" && index+1 < len(spec.Args) {
				f.session = spec.Args[index+1]
			}
		}
		return nil
	})
	go f.serve(proc)
	return proc, nil
}

func (f *fakeClaude) serve(proc *fakeProcess) {
	scanner := bufio.NewScanner(proc.serverInput)
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		_ = lockscope.WithMutexLock(&f.mu, func() error {
			f.received = append(f.received, message)
			return nil
		})
		switch message["type"] {
		case "control_request":
			request, _ := message["request"].(map[string]any)
			requestID, _ := message["request_id"].(string)
			switch request["subtype"] {
			case "initialize":
				f.write(fmt.Sprintf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":%s}}`, requestID, f.initialize))
			case "interrupt":
				f.write(fmt.Sprintf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`, requestID))
			}
		case "user":
			if f.onUser != nil {
				f.onUser(f, message)
			}
		}
	}
	if f.exitOnStdinClose {
		proc.exit()
	}
}

// write sends one native stdout line; it reports nothing once the pipe closed.
func (f *fakeClaude) write(line string) {
	proc := f.process()
	_, _ = proc.serverOutput.Write([]byte(line + "\n"))
}

// writeFixture writes an ndjson fixture with its placeholders replaced.
func (f *fakeClaude) writeFixture(name string, vars map[string]string) {
	replacements := []string{"{{SESSION}}", f.nativeSession()}
	for key, value := range vars {
		replacements = append(replacements, "{{"+key+"}}", value)
	}
	text := strings.NewReplacer(replacements...).Replace(readFixture(f.t, name))
	for _, line := range strings.Split(text, "\n") {
		f.write(line)
	}
}

func (f *fakeClaude) nativeSession() string {
	var session string
	_ = lockscope.WithMutexLock(&f.mu, func() error {
		session = f.session
		return nil
	})
	return session
}

func (f *fakeClaude) launchSpec() (processruntime.Spec, int) {
	var spec processruntime.Spec
	var starts int
	_ = lockscope.WithMutexLock(&f.mu, func() error {
		spec, starts = f.spec, f.starts
		return nil
	})
	return spec, starts
}

func (f *fakeClaude) messages(kind string) []map[string]any {
	var result []map[string]any
	_ = lockscope.WithMutexLock(&f.mu, func() error {
		for _, message := range f.received {
			if message["type"] == kind {
				result = append(result, message)
			}
		}
		return nil
	})
	return result
}

func (f *fakeClaude) controlSubtypes() []string {
	var subtypes []string
	for _, message := range f.messages("control_request") {
		request, _ := message["request"].(map[string]any)
		subtype, _ := request["subtype"].(string)
		subtypes = append(subtypes, subtype)
	}
	return subtypes
}

func (f *fakeClaude) process() *fakeProcess {
	var proc *fakeProcess
	_ = lockscope.WithMutexLock(&f.mu, func() error {
		proc = f.proc
		return nil
	})
	return proc
}

type fakeProcess struct {
	stdin        *io.PipeWriter
	stdout       *io.PipeReader
	stderr       *io.PipeReader
	serverInput  *io.PipeReader
	serverOutput *io.PipeWriter
	stderrWriter *io.PipeWriter
	done         chan struct{}
	exitOnce     sync.Once
	killed       atomic.Bool
	waited       atomic.Bool
	interrupts   atomic.Int32
}

func (*fakeProcess) PID() int                { return 42 }
func (p *fakeProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *fakeProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *fakeProcess) Interrupt() error      { p.interrupts.Add(1); return nil }
func (*fakeProcess) TreeKillSupported() bool { return true }

// exit closes every server end, like a process whose group terminated.
func (p *fakeProcess) exit() {
	p.exitOnce.Do(func() {
		_ = p.serverInput.Close()
		_ = p.serverOutput.Close()
		_ = p.stderrWriter.Close()
		close(p.done)
	})
}

func (p *fakeProcess) KillTree() error {
	select {
	case <-p.done:
		return os.ErrProcessDone
	default:
	}
	p.killed.Store(true)
	p.exit()
	return nil
}

func (p *fakeProcess) Wait() processruntime.Result {
	<-p.done
	p.waited.Store(true)
	return processruntime.Result{ExitedAt: time.Now()}
}

func (p *fakeProcess) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Test fixtures and helpers.

var (
	testModel      = provider.ModelSelection{ProviderID: "anthropic", ModelID: "claude-test-model"}
	noneContract   = provider.ResultContractRef{Requested: provider.SchemaNone}
	schemaContract = provider.ResultContractRef{Schema: json.RawMessage(`{"type":"object"}`), Requested: provider.SchemaNative}
)

func testAssignment(revision uint64) provider.AssignmentRef {
	return sessions.AssignmentRef{ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: revision}
}

func testConfig(fake *fakeClaude) Config {
	var ids, uuids atomic.Uint64
	return Config{
		ExecutablePath: "/test/claude", Launcher: fake, StopGrace: 200 * time.Millisecond,
		Environ: func() []string {
			return []string{"PATH=/bin", "ANTHROPIC_API_KEY=secret", "CLAUDE_CODE_ENTRYPOINT=cli"}
		},
		Now:   func() time.Time { return time.Unix(100, 0) },
		NewID: func(prefix string) (string, error) { return fmt.Sprintf("%s-%d", prefix, ids.Add(1)), nil },
		NewUUID: func() (string, error) {
			return fmt.Sprintf("00000000-0000-4000-8000-%012d", uuids.Add(1)), nil
		},
	}
}

func openRequest(t *testing.T, contract provider.ResultContractRef) provider.OpenSessionRequest {
	return provider.OpenSessionRequest{
		RequestID: "open", Assignment: testAssignment(1),
		Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: true},
		Model:     testModel, ResultContract: contract, PermissionMode: provider.PermissionDeny,
	}
}

// openTestSession opens a session against fake and subscribes to its events.
func openTestSession(t *testing.T, fake *fakeClaude, contract provider.ResultContractRef) (provider.Session, provider.EventStream) {
	t.Helper()
	opened, err := New(testConfig(fake)).OpenSession(context.Background(), openRequest(t, contract))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := opened.Session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
	})
	return opened.Session, stream
}

func startTurn(t *testing.T, s provider.Session, contract provider.ResultContractRef) provider.StartTurnResult {
	t.Helper()
	started, err := s.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "start", Session: s.Ref(), Assignment: testAssignment(1),
		Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "do work"}}, ResultContract: contract,
	})
	if err != nil {
		t.Fatal(err)
	}
	return started
}

// receiveUntil collects events until match returns true for one of them.
func receiveUntil(t *testing.T, stream provider.EventStream, match func(provider.ProviderEvent) bool) []provider.ProviderEvent {
	t.Helper()
	var events []provider.ProviderEvent
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("stream ended before the expected event: %v (received %d events)", err, len(events))
		}
		events = append(events, event)
		if match(event) {
			return events
		}
	}
}

// receiveUntilQuiet collects events until none arrives within quiet.
func receiveUntilQuiet(stream provider.EventStream, quiet time.Duration) ([]provider.ProviderEvent, error) {
	var events []provider.ProviderEvent
	for {
		ctx, cancel := context.WithTimeout(context.Background(), quiet)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return events, nil
			}
			return events, err
		}
		events = append(events, event)
	}
}

func isTurnTerminal(event provider.ProviderEvent) bool {
	body, ok := event.Body().(provider.TurnStateEvent)
	return ok && body.Current == provider.TurnNativeTerminal
}

func warningCodes(events []provider.ProviderEvent) []string {
	var codes []string
	for _, event := range events {
		if warning, ok := event.Body().(provider.ProtocolWarningEvent); ok {
			codes = append(codes, warning.Code)
		}
	}
	return codes
}

func hasWarning(events []provider.ProviderEvent, code string) bool {
	for _, got := range warningCodes(events) {
		if got == code {
			return true
		}
	}
	return false
}

func bodiesOf[T provider.EventBody](events []provider.ProviderEvent) []T {
	var result []T
	for _, event := range events {
		if body, ok := event.Body().(T); ok {
			result = append(result, body)
		}
	}
	return result
}

func messageUUID(message map[string]any) string {
	uuid, _ := message["uuid"].(string)
	return uuid
}
