package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// ErrMalformedLine reports a native line that is not a JSON-RPC object. ACP
// has no recovery point inside a broken stream, so it stops the transport.
var ErrMalformedLine = errors.New("malformed ACP JSON-RPC line")

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type writeJob struct {
	payload []byte
	result  chan error
	// taken closes when the writer dequeues the job; from then on the writer
	// always reports its outcome, even if the transport stops meanwhile.
	taken chan struct{}
}

func newWriteJob(payload []byte) writeJob {
	return writeJob{payload: payload, result: make(chan error, 1), taken: make(chan struct{})}
}

type rpcEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// rpcError is a native JSON-RPC error object. Error omits Data so native
// diagnostics do not reach logs through error strings.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("native JSON-RPC error %d: %s", e.Code, e.Message)
}

// rpcClient owns newline-delimited JSON-RPC 2.0 over the agent's stdio. One
// writer goroutine serializes output; one reader goroutine dispatches input.
// Reply callbacks run on the reader goroutine (or once on transport stop), so
// state derived from a reply is committed before the next native line.
type rpcClient struct {
	process      processruntime.Process
	writer       *bufio.Writer
	maxLineBytes int

	mu       sync.Mutex
	nextID   uint64
	pending  map[uint64]func(rpcResponse)
	closed   bool
	closeErr error
	writes   chan writeJob
	done     chan struct{}
	// finished closes after finish has failed pending replies and run onClosed.
	finished chan struct{}

	onNotification func(method string, params json.RawMessage)
	onRequest      func(id json.RawMessage, method string, params json.RawMessage)
	// onClosed runs once, outside rpc locks, after pending replies were failed.
	onClosed func(error)
}

func newRPCClient(proc processruntime.Process, maxLineBytes int) *rpcClient {
	return &rpcClient{
		process:      proc,
		writer:       bufio.NewWriter(proc.Stdin()),
		maxLineBytes: maxLineBytes,
		nextID:       1,
		pending:      make(map[uint64]func(rpcResponse)),
		writes:       make(chan writeJob, 64),
		done:         make(chan struct{}),
		finished:     make(chan struct{}),
	}
}

func (c *rpcClient) start() {
	go c.writeLoop()
	go c.readLoop()
	go func() { _, _ = io.Copy(io.Discard, c.process.Stderr()) }()
}

// call sends a request and waits for its reply. A caller that stops waiting
// after dispatch leaves the reply to be dropped; sent reports possible dispatch.
func (c *rpcClient) call(ctx context.Context, method string, params any) (json.RawMessage, bool, error) {
	return c.callCommitting(ctx, method, params, nil)
}

// callCommitting is call with a commit hook that observes the reply on the
// reader goroutine before any later native line is processed.
func (c *rpcClient) callCommitting(ctx context.Context, method string, params any, commit func(rpcResponse)) (json.RawMessage, bool, error) {
	replies := make(chan rpcResponse, 1)
	sent, err := c.send(ctx, method, params, func(reply rpcResponse) {
		if commit != nil {
			commit(reply)
		}
		replies <- reply
	})
	if err != nil {
		return nil, sent, err
	}
	select {
	case <-ctx.Done():
		return nil, true, ctx.Err()
	case reply := <-replies:
		return reply.result, true, reply.err
	}
}

// send registers onReply, queues the request and waits only for the write.
// onReply is called exactly once unless the request is proven unsent: with
// the native reply, or with the transport error when the transport stops.
func (c *rpcClient) send(ctx context.Context, method string, params any, onReply func(rpcResponse)) (sent bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var id uint64
	err = lockscope.WithMutexLock(&c.mu, func() error {
		if c.closed {
			return c.closeErr
		}
		id = c.nextID
		c.nextID++
		c.pending[id] = onReply
		return nil
	})
	if err != nil {
		return false, err
	}
	encoded, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		c.removePending(id)
		return false, err
	}
	job := newWriteJob(append(encoded, '\n'))
	select {
	case <-ctx.Done():
		c.removePending(id)
		return false, ctx.Err()
	case <-c.done:
		c.removePending(id)
		return false, c.terminalErr()
	case c.writes <- job:
	}
	// Once queued to the owned writer the request may reach the agent after
	// this caller stops waiting, so every later failure is "possibly sent".
	return true, c.awaitWrite(ctx, job)
}

func (c *rpcClient) notify(ctx context.Context, method string, params any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return false, err
	}
	return c.dispatch(ctx, append(encoded, '\n'))
}

func (c *rpcClient) respond(ctx context.Context, id json.RawMessage, result any) error {
	encoded, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		return err
	}
	_, err = c.dispatch(ctx, append(encoded, '\n'))
	return err
}

func (c *rpcClient) respondError(ctx context.Context, id json.RawMessage, code int, message string) error {
	encoded, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcError        `json:"error"`
	}{JSONRPC: "2.0", ID: id, Error: rpcError{Code: code, Message: message}})
	if err != nil {
		return err
	}
	_, err = c.dispatch(ctx, append(encoded, '\n'))
	return err
}

func (c *rpcClient) dispatch(ctx context.Context, payload []byte) (bool, error) {
	job := newWriteJob(payload)
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-c.done:
		return false, c.terminalErr()
	case c.writes <- job:
	}
	return true, c.awaitWrite(ctx, job)
}

// awaitWrite waits for the writer's outcome. A job the writer already took
// reports its real write result even when the transport stops right after
// the write; a job still queued when the transport stops is never written and
// reports the terminal transport error.
func (c *rpcClient) awaitWrite(ctx context.Context, job writeJob) error {
	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
	}
	select {
	case <-job.taken:
	default:
		return c.terminalErr()
	}
	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *rpcClient) terminalErr() error {
	var err error
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		err = c.closeErr
		return nil
	})
	if err == nil {
		return io.EOF
	}
	return err
}

func (c *rpcClient) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case job := <-c.writes:
			close(job.taken)
			_, err := c.writer.Write(job.payload)
			if err == nil {
				err = c.writer.Flush()
			}
			job.result <- err
			if err != nil {
				c.finish(fmt.Errorf("write ACP request: %w", err))
				return
			}
		}
	}
}

func (c *rpcClient) removePending(id uint64) {
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		delete(c.pending, id)
		return nil
	})
}

// readLoop stops the transport on EOF, a read error (including a line above
// maxLineBytes) or a malformed line.
func (c *rpcClient) readLoop() {
	scanner := bufio.NewScanner(c.process.Stdout())
	scanner.Buffer(make([]byte, 0, min(64*1024, c.maxLineBytes)), c.maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			c.finish(fmt.Errorf("%w: %w", ErrMalformedLine, err))
			_, _ = io.Copy(io.Discard, c.process.Stdout())
			return
		}
		c.route(envelope)
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	} else {
		// Keep draining so the owned process never blocks on a full pipe
		// before it is terminated.
		defer func() { _, _ = io.Copy(io.Discard, c.process.Stdout()) }()
	}
	c.finish(err)
}

func (c *rpcClient) route(envelope rpcEnvelope) {
	hasID := len(envelope.ID) != 0 && string(envelope.ID) != "null"
	switch {
	case envelope.Method != "" && hasID:
		if c.onRequest != nil {
			c.onRequest(envelope.ID, envelope.Method, envelope.Params)
		}
	case envelope.Method != "":
		if c.onNotification != nil {
			c.onNotification(envelope.Method, envelope.Params)
		}
	case hasID:
		var id uint64
		if json.Unmarshal(envelope.ID, &id) == nil {
			c.deliverResponse(id, envelope)
		}
	}
}

func (c *rpcClient) deliverResponse(id uint64, envelope rpcEnvelope) {
	var onReply func(rpcResponse)
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		onReply = c.pending[id]
		delete(c.pending, id)
		return nil
	})
	if onReply == nil {
		return
	}
	if envelope.Error != nil {
		onReply(rpcResponse{err: envelope.Error})
		return
	}
	onReply(rpcResponse{result: envelope.Result})
}

// finish stops the transport once, fails every pending reply with cause and
// then reports the stop.
func (c *rpcClient) finish(cause error) {
	var pending map[uint64]func(rpcResponse)
	first := false
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		if c.closed {
			return nil
		}
		c.closed = true
		c.closeErr = cause
		pending = c.pending
		c.pending = make(map[uint64]func(rpcResponse))
		close(c.done)
		first = true
		return nil
	})
	if !first {
		return
	}
	for _, onReply := range pending {
		onReply(rpcResponse{err: &transportError{cause: cause}})
	}
	if c.onClosed != nil {
		c.onClosed(cause)
	}
	close(c.finished)
}

func (c *rpcClient) stopped() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *rpcClient) closeInput() error {
	err := c.process.Stdin().Close()
	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}

// transportError marks a reply failed by transport stop, not by the agent.
type transportError struct{ cause error }

func (e *transportError) Error() string { return "ACP transport stopped: " + e.cause.Error() }
func (e *transportError) Unwrap() error { return e.cause }
