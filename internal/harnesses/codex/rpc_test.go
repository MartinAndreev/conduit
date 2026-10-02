package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

type rpcTestProcess struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (p *rpcTestProcess) PID() int                    { return 1 }
func (p *rpcTestProcess) Stdin() io.WriteCloser       { return p.stdin }
func (p *rpcTestProcess) Stdout() io.ReadCloser       { return p.stdout }
func (p *rpcTestProcess) Stderr() io.ReadCloser       { return p.stderr }
func (p *rpcTestProcess) Wait() processruntime.Result { return processruntime.Result{} }
func (p *rpcTestProcess) Interrupt() error            { return nil }
func (p *rpcTestProcess) KillTree() error             { return nil }
func (p *rpcTestProcess) TreeKillSupported() bool     { return true }

type recordingWriteCloser struct {
	mu     sync.Mutex
	writes int
	err    error
	n      int
}

func (w *recordingWriteCloser) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	if w.err != nil {
		return w.n, w.err
	}
	return len(value), nil
}
func (*recordingWriteCloser) Close() error { return nil }

func (w *recordingWriteCloser) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

type blockedWriteCloser struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedWriteCloser) Write(value []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(value), nil
}
func (*blockedWriteCloser) Close() error { return nil }

func newRPCProcess(stdin io.WriteCloser, stdout io.ReadCloser) *rpcTestProcess {
	return &rpcTestProcess{stdin: stdin, stdout: stdout, stderr: io.NopCloser(&emptyReader{})}
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

func TestRPCCallCancelledBeforeDispatchDoesNotWrite(t *testing.T) {
	writer := &recordingWriteCloser{}
	client := newRPCClient(newRPCProcess(writer, io.NopCloser(&emptyReader{})))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, sent, err := client.call(ctx, "test", struct{}{}, nil)
	if !errors.Is(err, context.Canceled) || sent {
		t.Fatalf("call = sent %v, err %v; want not sent cancellation", sent, err)
	}
	if got := writer.count(); got != 0 {
		t.Fatalf("writes = %d, want 0", got)
	}
}

func TestRPCCallPartialWriteIsPossiblySent(t *testing.T) {
	writeErr := errors.New("partial write")
	writer := &recordingWriteCloser{n: 3, err: writeErr}
	stdoutReader, stdoutWriter := io.Pipe()
	client := newRPCClient(newRPCProcess(writer, stdoutReader))
	client.start(nil, nil)
	defer stdoutWriter.Close()
	_, sent, err := client.call(context.Background(), "test", struct{}{}, nil)
	if !sent || !errors.Is(err, writeErr) {
		t.Fatalf("call = sent %v, err %v; want possibly sent partial-write error", sent, err)
	}
}

func TestRPCCallBlockedWriterHonorsDeadline(t *testing.T) {
	writer := &blockedWriteCloser{started: make(chan struct{}), release: make(chan struct{})}
	stdoutReader, stdoutWriter := io.Pipe()
	client := newRPCClient(newRPCProcess(writer, stdoutReader))
	client.start(nil, nil)
	defer stdoutWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	_, sent, err := client.call(ctx, "test", struct{}{}, nil)
	if !sent || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("call = sent %v, err %v; want delivery-unknown deadline", sent, err)
	}
	if time.Since(startedAt) > time.Second {
		t.Fatal("call remained blocked in writer past deadline")
	}
	close(writer.release)
}

func TestRPCLateResponseReconcilesAbandonedCall(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	client := newRPCClient(newRPCProcess(stdinWriter, stdoutReader))
	client.start(nil, nil)
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	requestID := make(chan uint64, 1)
	go func() {
		line, _ := bufio.NewReader(stdinReader).ReadBytes('\n')
		var request struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(line, &request)
		requestID <- request.ID
	}()
	late := make(chan rpcResponse, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, sent, err := client.call(ctx, "turn/steer", struct{}{}, func(response rpcResponse) { late <- response })
	if !sent || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("call = sent %v, err %v; want timed out possibly sent", sent, err)
	}
	id := <-requestID
	if _, err := stdoutWriter.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonNumber(id) + `,"result":{"turnId":"turn-1"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-late:
		if response.err != nil || string(response.result) != `{"turnId":"turn-1"}` {
			t.Fatalf("late response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("late response was discarded")
	}
}

func TestRPCEOFReleasesPendingCallAsUnknown(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	client := newRPCClient(newRPCProcess(stdinWriter, stdoutReader))
	client.start(nil, nil)
	defer stdinReader.Close()
	go func() {
		_, _ = bufio.NewReader(stdinReader).ReadBytes('\n')
		_ = stdoutWriter.Close()
	}()
	_, sent, err := client.call(context.Background(), "turn/start", struct{}{}, nil)
	if !sent || !errors.Is(err, io.EOF) {
		t.Fatalf("call = sent %v, err %v; want possibly sent EOF", sent, err)
	}
}

func jsonNumber(value uint64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestRPCMalformedLineIsVisibleAndReaderContinues(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	client := newRPCClient(newRPCProcess(stdinWriter, stdoutReader))
	notifications := make(chan string, 2)
	client.start(func(method string, _ json.RawMessage) { notifications <- method }, nil)
	defer stdinReader.Close()
	defer stdoutWriter.Close()
	_, _ = stdoutWriter.Write([]byte("not-json\n"))
	_, _ = stdoutWriter.Write([]byte(`{"jsonrpc":"2.0","method":"thread/name/updated","params":{}}` + "\n"))
	for _, want := range []string{"$protocol/malformed", "thread/name/updated"} {
		select {
		case got := <-notifications:
			if got != want {
				t.Fatalf("notification = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("notification %q not delivered", want)
		}
	}
}
