package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// errMalformedLine reports a stdout line that is not a JSON object. The
// stream-json protocol has no recovery point, so it stops the transport.
var errMalformedLine = errors.New("claude emitted a malformed stream-json line")

// nativeControlError is a control_response with subtype error: a proven
// native rejection rather than transport uncertainty.
type nativeControlError struct{ Message string }

func (e *nativeControlError) Error() string {
	return "native control request rejected: " + e.Message
}

type controlReply struct {
	response json.RawMessage
	err      error
}

type writeJob struct {
	payload []byte
	result  chan error
}

// inboundEnvelope decodes only the fields the transport routes on.
type inboundEnvelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype string `json:"subtype"`
	} `json:"request"`
	Response *struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
		Error     string          `json:"error"`
	} `json:"response"`
}

// transport owns the stream-json pipes of one claude process: one writer
// goroutine, one stdout reader, continuous stderr draining into a bounded tail,
// and control request/response correlation.
type transport struct {
	process      processruntime.Process
	writer       *bufio.Writer
	maxLineBytes int
	stderrTail   *tailBuffer

	mu       sync.Mutex
	nextID   uint64
	pending  map[string]chan controlReply
	closed   bool
	closeErr error
	writes   chan writeJob
	done     chan struct{}

	// Handlers run on the reader goroutine in wire order. Set them before start.
	onMessage        func(messageType string, line []byte)
	onControlRequest func(requestID, subtype string)
	// onClosed is called once, outside transport locks, when the transport
	// stops (reader EOF/error, a malformed or oversized line, a failed write).
	onClosed func(error)
}

func newTransport(proc processruntime.Process, maxLineBytes, stderrTailBytes int) *transport {
	return &transport{
		process:      proc,
		writer:       bufio.NewWriter(proc.Stdin()),
		maxLineBytes: maxLineBytes,
		stderrTail:   newTailBuffer(stderrTailBytes),
		nextID:       1,
		pending:      make(map[string]chan controlReply),
		writes:       make(chan writeJob, 64),
		done:         make(chan struct{}),
	}
}

func (t *transport) start() {
	go t.writeLoop()
	go t.readLoop()
	go func() { _, _ = io.Copy(t.stderrTail, t.process.Stderr()) }()
}

// send writes one message. sent is true once the message reached the owned
// writer, after which delivery may happen even if the caller stops waiting.
func (t *transport) send(ctx context.Context, message any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return false, err
	}
	job := writeJob{payload: append(encoded, '\n'), result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-t.done:
		return false, t.terminalErr()
	case t.writes <- job:
	}
	return true, t.awaitWrite(ctx, job)
}

// control sends one control_request and waits for its control_response. A
// native error response is a *nativeControlError; a reply that arrives after
// the caller stopped waiting is dropped, because both control requests used
// here (initialize, interrupt) carry no state that needs late reconciliation.
func (t *transport) control(ctx context.Context, request any) (json.RawMessage, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var requestID string
	var reply chan controlReply
	err := lockscope.WithMutexLock(&t.mu, func() error {
		if t.closed {
			if t.closeErr != nil {
				return t.closeErr
			}
			return io.EOF
		}
		requestID = "conduit_req_" + strconv.FormatUint(t.nextID, 10)
		t.nextID++
		reply = make(chan controlReply, 1)
		t.pending[requestID] = reply
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	message := struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   any    `json:"request"`
	}{Type: "control_request", RequestID: requestID, Request: request}
	sent, err := t.send(ctx, message)
	if err != nil {
		t.removePending(requestID)
		return nil, sent, err
	}
	select {
	case <-ctx.Done():
		t.removePending(requestID)
		return nil, true, ctx.Err()
	case result := <-reply:
		return result.response, true, result.err
	}
}

// rejectControlRequest answers a control_request sent by claude that this
// adapter does not support, so the native side does not wait for it.
func (t *transport) rejectControlRequest(ctx context.Context, requestID, message string) error {
	response := struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
		} `json:"response"`
	}{Type: "control_response"}
	response.Response.Subtype = "error"
	response.Response.RequestID = requestID
	response.Response.Error = message
	_, err := t.send(ctx, response)
	return err
}

// awaitWrite waits for the writer's outcome. A job still queued when the
// transport stops reports the terminal error, so no caller waits forever.
func (t *transport) awaitWrite(ctx context.Context, job writeJob) error {
	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		select {
		case err := <-job.result:
			return err
		default:
			return t.terminalErr()
		}
	}
}

func (t *transport) writeLoop() {
	for {
		select {
		case <-t.done:
			return
		case job := <-t.writes:
			_, err := t.writer.Write(job.payload)
			if err == nil {
				err = t.writer.Flush()
			}
			job.result <- err
			if err != nil {
				t.finish(fmt.Errorf("write claude stdin: %w", err))
				return
			}
		}
	}
}

func (t *transport) readLoop() {
	stdout := t.process.Stdout()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, min(64*1024, t.maxLineBytes)), t.maxLineBytes)
	var stopErr error
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var envelope inboundEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			stopErr = errors.Join(errMalformedLine, err)
			break
		}
		t.route(envelope, append([]byte(nil), line...))
	}
	if stopErr == nil {
		stopErr = scanner.Err()
	}
	if stopErr == nil {
		stopErr = io.EOF
	}
	t.finish(stopErr)
	// Keep draining so the owned process never blocks on a full stdout pipe and
	// Process.Wait can observe the pipe's end.
	_, _ = io.Copy(io.Discard, stdout)
}

func (t *transport) route(envelope inboundEnvelope, line []byte) {
	switch envelope.Type {
	case "control_response":
		if envelope.Response != nil {
			t.deliverControlResponse(envelope)
		}
	case "control_request":
		if t.onControlRequest != nil {
			t.onControlRequest(envelope.RequestID, envelope.Request.Subtype)
		}
	case "control_cancel_request":
		// Cancels a native control request this adapter already rejected.
	default:
		if t.onMessage != nil {
			t.onMessage(envelope.Type, line)
		}
	}
}

func (t *transport) deliverControlResponse(envelope inboundEnvelope) {
	var reply chan controlReply
	_ = lockscope.WithMutexLock(&t.mu, func() error {
		reply = t.pending[envelope.Response.RequestID]
		delete(t.pending, envelope.Response.RequestID)
		return nil
	})
	if reply == nil {
		return
	}
	if envelope.Response.Subtype == "error" {
		reply <- controlReply{err: &nativeControlError{Message: envelope.Response.Error}}
		return
	}
	reply <- controlReply{response: envelope.Response.Response}
}

func (t *transport) removePending(requestID string) {
	_ = lockscope.WithMutexLock(&t.mu, func() error {
		delete(t.pending, requestID)
		return nil
	})
}

func (t *transport) terminalErr() error {
	var err error
	_ = lockscope.WithMutexLock(&t.mu, func() error {
		err = t.closeErr
		return nil
	})
	if err == nil {
		return io.EOF
	}
	return err
}

func (t *transport) finish(err error) {
	var pending map[string]chan controlReply
	first := false
	_ = lockscope.WithMutexLock(&t.mu, func() error {
		if t.closed {
			return nil
		}
		t.closed = true
		t.closeErr = err
		pending = t.pending
		t.pending = make(map[string]chan controlReply)
		close(t.done)
		first = true
		return nil
	})
	if !first {
		return
	}
	for _, reply := range pending {
		reply <- controlReply{err: err}
	}
	if t.onClosed != nil {
		t.onClosed(err)
	}
}

func (t *transport) closeInput() error {
	err := t.process.Stdin().Close()
	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}

// tailBuffer keeps the last limit bytes written to it. Writes never fail, so
// stderr is always drained.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{limit: limit} }

func (b *tailBuffer) Write(value []byte) (int, error) {
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		b.data = append(b.data, value...)
		if excess := len(b.data) - b.limit; excess > 0 {
			b.data = append(b.data[:0], b.data[excess:]...)
		}
		return nil
	})
	return len(value), nil
}

func (b *tailBuffer) String() string {
	var text string
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		text = string(b.data)
		return nil
	})
	return text
}
