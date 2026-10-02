package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type pendingRequest struct {
	response  chan rpcResponse
	abandoned bool
	late      func(rpcResponse)
}

type writeJob struct {
	payload []byte
	result  chan error
}

type rpcEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("native JSON-RPC error %d: %s", e.Code, e.Message)
}

type rpcClient struct {
	process processruntime.Process
	writer  *bufio.Writer

	mu       sync.Mutex
	nextID   uint64
	pending  map[uint64]*pendingRequest
	closed   bool
	closeErr error
	writes   chan writeJob

	onNotification func(string, json.RawMessage)
	onRequest      func(json.RawMessage, string, json.RawMessage)
}

func newRPCClient(proc processruntime.Process) *rpcClient {
	return &rpcClient{
		process: proc,
		writer:  bufio.NewWriter(proc.Stdin()),
		nextID:  1,
		pending: make(map[uint64]*pendingRequest),
		writes:  make(chan writeJob, 64),
	}
}

func (c *rpcClient) start(onNotification func(string, json.RawMessage), onRequest func(json.RawMessage, string, json.RawMessage)) {
	c.onNotification = onNotification
	c.onRequest = onRequest
	go c.writeLoop()
	go c.readLoop()
	go io.Copy(io.Discard, c.process.Stderr())
}

// call returns sent=true once the complete request has been flushed to stdin.
func (c *rpcClient) call(ctx context.Context, method string, params any, late func(rpcResponse)) (result json.RawMessage, sent bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var id uint64
	var response chan rpcResponse
	err = lockscope.WithMutexLock(&c.mu, func() error {
		if c.closed {
			if c.closeErr != nil {
				return c.closeErr
			}
			return io.EOF
		}
		id = c.nextID
		c.nextID++
		response = make(chan rpcResponse, 1)
		c.pending[id] = &pendingRequest{response: response}
		return nil
	})
	if err != nil {
		return nil, false, err
	}

	payload := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	encoded, err := json.Marshal(payload)
	if err != nil {
		c.removePending(id)
		return nil, false, err
	}
	encoded = append(encoded, '\n')
	job := writeJob{payload: encoded, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		c.removePending(id)
		return nil, false, ctx.Err()
	case c.writes <- job:
		// Once queued to the owned writer, dispatch may happen after this caller
		// stops waiting. Conservatively report possibly sent.
		sent = true
	}
	select {
	case err := <-job.result:
		if err != nil {
			c.removePending(id)
			return nil, true, err
		}
	case <-ctx.Done():
		c.abandon(id, response, late)
		return nil, true, ctx.Err()
	}

	select {
	case <-ctx.Done():
		c.abandon(id, response, late)
		return nil, true, ctx.Err()
	case reply := <-response:
		return reply.result, true, reply.err
	}
}

func (c *rpcClient) notify(ctx context.Context, method string, params any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	payload := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", Method: method, Params: params}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	return c.dispatch(ctx, append(encoded, '\n'))
}

func (c *rpcClient) rejectRequest(ctx context.Context, id json.RawMessage, code int, message string) error {
	payload := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcError        `json:"error"`
	}{JSONRPC: "2.0", ID: id, Error: rpcError{Code: code, Message: message}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.dispatch(ctx, append(encoded, '\n'))
	return err
}

func (c *rpcClient) dispatch(ctx context.Context, payload []byte) (bool, error) {
	job := writeJob{payload: payload, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case c.writes <- job:
	}
	select {
	case err := <-job.result:
		return true, err
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

func (c *rpcClient) writeLoop() {
	for job := range c.writes {
		_, err := c.writer.Write(job.payload)
		if err == nil {
			err = c.writer.Flush()
		}
		job.result <- err
		if err != nil {
			c.finish(err)
			return
		}
	}
}

func (c *rpcClient) removePending(id uint64) {
	_ = lockscope.WithMutexLock(&c.mu, func() error { delete(c.pending, id); return nil })
}

func (c *rpcClient) abandon(id uint64, response chan rpcResponse, late func(rpcResponse)) {
	var registered bool
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		if pending := c.pending[id]; pending != nil {
			pending.abandoned = true
			pending.late = late
			registered = true
		}
		return nil
	})
	if registered {
		return
	}
	select {
	case reply := <-response:
		if late != nil {
			late(reply)
		}
	default:
	}
}

func (c *rpcClient) readLoop() {
	scanner := bufio.NewScanner(c.process.Stdout())
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var envelope rpcEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			if c.onNotification != nil {
				c.onNotification("$protocol/malformed", append(json.RawMessage(nil), line...))
			}
			continue
		}
		if len(envelope.ID) != 0 && envelope.Method == "" {
			var id uint64
			if err := json.Unmarshal(envelope.ID, &id); err == nil {
				c.deliverResponse(id, envelope)
			}
			continue
		}
		if envelope.Method != "" && len(envelope.ID) != 0 {
			if c.onRequest != nil {
				c.onRequest(envelope.ID, envelope.Method, envelope.Params)
			}
			continue
		}
		if envelope.Method != "" && c.onNotification != nil {
			c.onNotification(envelope.Method, envelope.Params)
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.finish(err)
}

func (c *rpcClient) deliverResponse(id uint64, envelope rpcEnvelope) {
	var pending *pendingRequest
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		pending = c.pending[id]
		delete(c.pending, id)
		return nil
	})
	if pending == nil {
		return
	}
	reply := rpcResponse{result: envelope.Result}
	if envelope.Error != nil {
		reply = rpcResponse{err: envelope.Error}
	}
	if pending.abandoned {
		if pending.late != nil {
			pending.late(reply)
		}
		return
	}
	pending.response <- reply
}

func (c *rpcClient) finish(err error) {
	var pending map[uint64]*pendingRequest
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		if c.closed {
			return nil
		}
		c.closed = true
		c.closeErr = err
		pending = c.pending
		c.pending = make(map[uint64]*pendingRequest)
		return nil
	})
	for _, request := range pending {
		reply := rpcResponse{err: err}
		if request.abandoned {
			if request.late != nil {
				request.late(reply)
			}
			continue
		}
		request.response <- reply
	}
}

func (c *rpcClient) closeInput() error {
	err := c.process.Stdin().Close()
	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}
