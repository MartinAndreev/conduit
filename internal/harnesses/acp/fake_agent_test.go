package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// clientFrame is one JSON-RPC line written by the adapter.
type clientFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (f clientFrame) kind() string {
	switch {
	case f.Method != "" && len(f.ID) != 0:
		return "request"
	case f.Method != "":
		return "notification"
	default:
		return "response"
	}
}

// agentConn is the fake agent's side of the stdio pipes.
type agentConn struct {
	input  *bufio.Reader
	output *io.PipeWriter
	killed <-chan struct{}
	writeM sync.Mutex
	// descendantHoldsStdout, set by serve before it returns, makes the root
	// exit at once while stdout stays open until the group is killed.
	descendantHoldsStdout bool
}

func (c *agentConn) write(value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	c.writeRaw(string(encoded) + "\n")
}

func (c *agentConn) writeRaw(line string) {
	_ = lockscope.WithMutexLock(&c.writeM, func() error {
		_, err := c.output.Write([]byte(line))
		return err
	})
}

func (c *agentConn) reply(id json.RawMessage, result any) {
	c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *agentConn) replyError(id json.RawMessage, code int, message string, data any) {
	failure := map[string]any{"code": code, "message": message}
	if data != nil {
		failure["data"] = data
	}
	c.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": failure})
}

func (c *agentConn) notify(method string, params any) {
	c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *agentConn) update(sessionID string, update map[string]any) {
	c.notify("session/update", map[string]any{"sessionId": sessionID, "update": update})
}

func (c *agentConn) request(id any, method string, params any) {
	c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

// next reads the adapter's next frame; ok is false at stdin EOF.
func (c *agentConn) next() (clientFrame, bool) {
	line, err := c.input.ReadBytes('\n')
	if err != nil {
		return clientFrame{}, false
	}
	var frame clientFrame
	if json.Unmarshal(line, &frame) != nil {
		return clientFrame{Method: "$malformed"}, true
	}
	return frame, true
}

// fakeLauncher starts an in-process agent driven by serve.
type fakeLauncher struct {
	// killErr, when set, makes KillTree fail without terminating anything.
	killErr error
	mu      sync.Mutex
	serve   func(*agentConn)
	specs   []processruntime.Spec
	procs   []*fakeProcess
	frames  []clientFrame
}

func (l *fakeLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	proc := &fakeProcess{
		stdin: clientInput, stdout: clientOutput, stderr: io.NopCloser(eofReader{}),
		serverInput: serverInput, serverOutput: serverOutput, done: make(chan struct{}),
		killed: make(chan struct{}), rootExited: make(chan struct{}), killErr: l.killErr,
	}
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		l.specs = append(l.specs, spec)
		l.procs = append(l.procs, proc)
		return nil
	})
	recorder := &recordingReader{launcher: l, reader: serverInput}
	conn := &agentConn{input: bufio.NewReader(recorder), output: serverOutput, killed: proc.killed}
	// Like the production process: the root exits when serve returns (after
	// stdin EOF unless a descendant keeps stdout), and Wait completes only
	// after stdout closed as well.
	go func() {
		l.serve(conn)
		if !conn.descendantHoldsStdout {
			// Drain until the adapter closes stdin or kills the process.
			_, _ = io.Copy(io.Discard, conn.input)
		}
		close(proc.rootExited)
		if conn.descendantHoldsStdout {
			<-proc.killed
		}
		_ = serverOutput.Close()
		close(proc.done)
	}()
	return proc, nil
}

func (l *fakeLauncher) recorded() []clientFrame {
	var frames []clientFrame
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		frames = append(frames, l.frames...)
		return nil
	})
	return frames
}

func (l *fakeLauncher) framesFor(method string) []clientFrame {
	var matches []clientFrame
	for _, frame := range l.recorded() {
		if frame.Method == method {
			matches = append(matches, frame)
		}
	}
	return matches
}

func (l *fakeLauncher) process(t *testing.T) *fakeProcess {
	t.Helper()
	var proc *fakeProcess
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		if len(l.procs) > 0 {
			proc = l.procs[len(l.procs)-1]
		}
		return nil
	})
	if proc == nil {
		t.Fatal("no process was launched")
	}
	return proc
}

// recordingReader records every complete adapter line as it is read.
type recordingReader struct {
	launcher *fakeLauncher
	reader   io.Reader
	partial  []byte
}

func (r *recordingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.partial = append(r.partial, buffer[:n]...)
	for {
		index := -1
		for i, b := range r.partial {
			if b == '\n' {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		var frame clientFrame
		if json.Unmarshal(r.partial[:index], &frame) == nil {
			_ = lockscope.WithMutexLock(&r.launcher.mu, func() error {
				r.launcher.frames = append(r.launcher.frames, frame)
				return nil
			})
		}
		r.partial = r.partial[index+1:]
	}
	return n, err
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

type fakeProcess struct {
	stdin        *io.PipeWriter
	stdout       *io.PipeReader
	stderr       io.ReadCloser
	serverInput  *io.PipeReader
	serverOutput *io.PipeWriter
	done         chan struct{}
	rootExited   chan struct{}
	killed       chan struct{}
	killErr      error
	killOnce     sync.Once
	kills        atomic.Int32
	waits        atomic.Int32
}

func (*fakeProcess) PID() int                { return 4242 }
func (p *fakeProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *fakeProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *fakeProcess) Interrupt() error      { return nil }
func (*fakeProcess) TreeKillSupported() bool { return true }

func (p *fakeProcess) KillTree() error {
	p.kills.Add(1)
	if p.killErr != nil {
		return p.killErr
	}
	select {
	case <-p.done:
		return os.ErrProcessDone
	default:
	}
	p.killOnce.Do(func() {
		close(p.killed)
		_ = p.serverInput.CloseWithError(errors.New("killed"))
		_ = p.serverOutput.Close()
	})
	return nil
}

func (p *fakeProcess) Wait() processruntime.Result {
	p.waits.Add(1)
	<-p.done
	return processruntime.Result{ExitedAt: time.Now()}
}

// RootExited and RootResult implement processruntime.RootExitObserver.
func (p *fakeProcess) RootExited() <-chan struct{} { return p.rootExited }

func (p *fakeProcess) RootResult() processruntime.Result {
	<-p.rootExited
	return processruntime.Result{ExitedAt: time.Now()}
}

var _ processruntime.RootExitObserver = (*fakeProcess)(nil)

func (p *fakeProcess) reaped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

const testSessionID = "fx-session-01"

// standardAgent answers initialize and session/new like the fx fixtures and
// delegates every other frame to handle (nil ignores them).
func standardAgent(initialize map[string]any, created map[string]any, handle func(*agentConn, clientFrame)) func(*agentConn) {
	if initialize == nil {
		initialize = map[string]any{
			"protocolVersion": 1,
			"agentCapabilities": map[string]any{
				"loadSession":         true,
				"sessionCapabilities": map[string]any{"close": map[string]any{}},
			},
			"agentInfo":   map[string]any{"name": "fake", "version": "1.2.3"},
			"authMethods": []any{},
		}
	}
	if created == nil {
		created = map[string]any{"sessionId": testSessionID}
	}
	return func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch {
			case frame.Method == "initialize":
				conn.reply(frame.ID, initialize)
			case frame.Method == "session/new":
				conn.reply(frame.ID, created)
				conn.update(testSessionID, map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{}})
			case frame.Method == "session/close":
				conn.reply(frame.ID, nil)
			case handle != nil:
				handle(conn, frame)
			}
		}
	}
}

type testHarness struct {
	t        *testing.T
	launcher *fakeLauncher
	adapter  *Adapter
	session  provider.Session
	stream   provider.EventStream
	released chan struct{}
}

var testAssignment = provider.AssignmentRef{AssignmentID: "assignment-1", Revision: 1}

func testDescriptor() Descriptor {
	return Descriptor{
		ProviderName: "fake", AdapterVersion: "fake-acp/1", Executable: "fake-agent", Args: []string{"acp"},
		ParseVersion: func(value string) string { return value },
	}
}

func newTestAdapter(launcher *fakeLauncher, mutate func(*Config)) *Adapter {
	var ids atomic.Uint64
	config := Config{
		Launcher: launcher, StopGrace: 200 * time.Millisecond, EventBuffer: 512,
		NewID: func(prefix string) (string, error) { return prefix + "-" + strconv.FormatUint(ids.Add(1), 10), nil },
		Now:   func() time.Time { return time.Unix(1000, 0) },
	}
	if mutate != nil {
		mutate(&config)
	}
	return New(testDescriptor(), config)
}

func openRequest(workspace string) provider.OpenSessionRequest {
	return provider.OpenSessionRequest{
		RequestID: "open-1", Assignment: testAssignment,
		Workspace:      provider.WorkspaceBinding{WorkspaceID: "ws", Path: workspace, Writable: true},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
		PermissionMode: provider.PermissionDeny,
	}
}

func openHarness(t *testing.T, serve func(*agentConn), mutate func(*Config)) *testHarness {
	t.Helper()
	return openHarnessFull(t, &fakeLauncher{serve: serve}, mutate, nil)
}

// openHarnessFull opens a session with optional test hooks. It always adds a
// turn-release barrier so tests can start the next turn deterministically.
func openHarnessFull(t *testing.T, launcher *fakeLauncher, mutate func(*Config), hooks testHooks) *testHarness {
	t.Helper()
	adapter := newTestAdapter(launcher, mutate)
	released := make(chan struct{}, 64)
	adapter.hooks = testHooks{}
	for point, hook := range hooks {
		adapter.hooks[point] = hook
	}
	userReleased := hooks[hookTurnReleased]
	adapter.hooks[hookTurnReleased] = func(s *session) {
		if userReleased != nil {
			userReleased(s)
		}
		released <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opened, err := adapter.OpenSession(ctx, openRequest(t.TempDir()))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	h := &testHarness{t: t, launcher: launcher, adapter: adapter, session: opened.Session, released: released}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.session.Close(ctx, provider.CloseSessionRequest{Session: h.session.Ref(), Policy: provider.CloseInterruptThenClose})
	})
	return h
}

// awaitTurnReleased blocks until the next turn released its ending lease.
func (h *testHarness) awaitTurnReleased() {
	h.t.Helper()
	select {
	case <-h.released:
	case <-time.After(5 * time.Second):
		h.t.Fatal("timed out waiting for turn release")
	}
}

func (h *testHarness) events() provider.EventStream {
	h.t.Helper()
	if h.stream == nil {
		stream, err := h.session.Events(context.Background(), provider.EventCursor{})
		if err != nil {
			h.t.Fatalf("Events: %v", err)
		}
		h.stream = stream
	}
	return h.stream
}

// collectUntil receives events until stop matches or the stream ends.
func (h *testHarness) collectUntil(stop func(provider.ProviderEvent) bool) ([]provider.ProviderEvent, error) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var events []provider.ProviderEvent
	for {
		event, err := h.events().Receive(ctx)
		if err != nil {
			return events, err
		}
		events = append(events, event)
		if stop(event) {
			return events, nil
		}
	}
}

func isTurnTerminal(event provider.ProviderEvent) bool {
	body, ok := event.Body().(provider.TurnStateEvent)
	return ok && (body.Current == provider.TurnNativeTerminal || body.Current == provider.TurnLost)
}

func (h *testHarness) startTurn(ctx context.Context, text string) (provider.StartTurnResult, error) {
	return h.session.StartTurn(ctx, provider.StartTurnRequest{
		RequestID: "turn-request", Session: h.session.Ref(), Assignment: testAssignment,
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: text}},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
	})
}

func warnings(events []provider.ProviderEvent, code string) []provider.ProtocolWarningEvent {
	var matches []provider.ProtocolWarningEvent
	for _, event := range events {
		if body, ok := event.Body().(provider.ProtocolWarningEvent); ok && body.Code == code {
			matches = append(matches, body)
		}
	}
	return matches
}

func bodiesOf[T provider.EventBody](events []provider.ProviderEvent) []T {
	var matches []T
	for _, event := range events {
		if body, ok := event.Body().(T); ok {
			matches = append(matches, body)
		}
	}
	return matches
}

func requireCode(t *testing.T, err error, code provider.ErrorCode) *provider.ProviderError {
	t.Helper()
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
	return providerErr
}

func eventually(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func describe(events []provider.ProviderEvent) string {
	text := ""
	for _, event := range events {
		text += fmt.Sprintf("%s %+v\n", event.Kind(), event.Body())
	}
	return text
}
