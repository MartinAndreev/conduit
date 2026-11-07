package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// workspacePermissionRules is the session rule set for the only accepted mode:
// a writable workspace with PermissionDeny (no permission round-trip). OpenCode
// evaluates rules last-match-wins, so the leading allow-all grants in-workspace
// tools (shell, edit, ...) without asking, overriding an "ask" in user config
// that would otherwise hang the turn, and the later denies take precedence for
// leaving the workspace, web access, and questions. Order is load-bearing.
var workspacePermissionRules = []struct{ action, effect string }{
	{"*", "allow"},
	{"external_directory", "deny"},
	{"webfetch", "deny"},
	{"websearch", "deny"},
	{"question", "deny"},
}

type cliControl struct {
	executable string
	launcher   processruntime.Launcher
}

func (c *cliControl) CreateSession(ctx context.Context, request CreateSessionRequest) (CreateSessionResult, bool, error) {
	payload := struct {
		Title string `json:"title"`
		Model struct {
			ProviderID string `json:"providerID"`
			ID         string `json:"id"`
			Variant    string `json:"variant,omitempty"`
		} `json:"model"`
		Location struct {
			Directory string `json:"directory"`
		} `json:"location"`
		Permissions []struct {
			Action   string `json:"action"`
			Resource string `json:"resource"`
			Effect   string `json:"effect"`
		} `json:"permissions"`
	}{Title: request.Title}
	payload.Model.ProviderID, payload.Model.ID, payload.Model.Variant = request.Model.ProviderID, request.Model.ModelID, request.Model.Variant
	payload.Location.Directory = request.Directory
	for _, rule := range workspacePermissionRules {
		payload.Permissions = append(payload.Permissions, struct {
			Action   string `json:"action"`
			Resource string `json:"resource"`
			Effect   string `json:"effect"`
		}{Action: rule.action, Resource: "*", Effect: rule.effect})
	}
	raw, _ := json.Marshal(payload)
	out, sent, err := c.call(ctx, request.Directory, "api", "session.create", "--data", string(raw))
	if err != nil {
		return CreateSessionResult{}, sent, err
	}
	var response struct {
		Data struct {
			ID    provider.NativeSessionID `json:"id"`
			Model struct {
				ProviderID string `json:"providerID"`
				ID         string `json:"id"`
				Variant    string `json:"variant"`
			} `json:"model"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &response) != nil || response.Data.ID == "" {
		return CreateSessionResult{}, true, errors.New("invalid session.create response")
	}
	observed := provider.ModelSelection{ProviderID: response.Data.Model.ProviderID, ModelID: response.Data.Model.ID, Variant: response.Data.Model.Variant}
	return CreateSessionResult{SessionID: response.Data.ID, Model: observed, ModelVerified: observed.ProviderID != "" && observed.ModelID != ""}, true, nil
}

func (c *cliControl) Prompt(ctx context.Context, request PromptRequest) (PromptResult, bool, error) {
	raw, _ := json.Marshal(struct {
		ID       string `json:"id,omitempty"`
		Text     string `json:"text"`
		Delivery string `json:"delivery"`
	}{request.MessageID, request.Text, request.Delivery})
	out, sent, err := c.call(ctx, request.Directory, "api", "session.prompt", "--param", "sessionID="+string(request.SessionID), "--data", string(raw))
	if err != nil {
		return PromptResult{}, sent, err
	}
	var response struct {
		Data *struct {
			ID        provider.NativeReceiptID `json:"id"`
			SessionID provider.NativeSessionID `json:"sessionID"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &response) != nil || response.Data == nil || response.Data.ID == "" || (response.Data.SessionID != "" && response.Data.SessionID != request.SessionID) {
		return PromptResult{}, true, errors.New("invalid session.prompt response")
	}
	return PromptResult{Accepted: true, ReceiptID: response.Data.ID}, true, nil
}

// QuerySteerDelivery is the named native query step for an unresolved steer.
// It reads the message by ID (session.message.get): found proves it is in the
// history. Only a MessageNotFoundError naming that ID proves absence. Then the
// pending inbox (session.inbox.list) is read, because OpenCode admits a prompt
// to the inbox before it delivers it. Delivery can move the message from the
// inbox to the history between those two reads, so an empty inbox is followed
// by a second history read: absence is reported only for the sequence history
// miss, inbox miss, history miss, and any history hit means delivered.
//
// Limitation: this assumes the inbox-to-history transfer is monotonic and
// never leaves the message in neither place mid-transfer; OpenCode does not
// document that guarantee. Any other outcome is a query failure, and no read
// changes the session.
func (c *cliControl) QuerySteerDelivery(ctx context.Context, query SteerDeliveryQuery) (SteerDeliveryObservation, error) {
	found, err := c.queryMessageInHistory(ctx, query)
	if err != nil || found {
		return SteerDeliveryObservation{InHistory: found}, err
	}
	pending, err := c.queryMessageInInbox(ctx, query)
	if err != nil || pending {
		return SteerDeliveryObservation{InInbox: pending}, err
	}
	found, err = c.queryMessageInHistory(ctx, query)
	if err != nil {
		return SteerDeliveryObservation{}, err
	}
	return SteerDeliveryObservation{InHistory: found}, nil
}

// releasedHistoryPageSize and releasedHistoryMaxPages bound the history scan
// of QueryReleasedSteer; a longer history is a query failure, never absence.
const (
	releasedHistoryPageSize = 100
	releasedHistoryMaxPages = 50
)

// QueryReleasedSteer is the named native query step that settles the
// provenance of an explicitly released steer. It scans the session message
// history (session.message.list) newest first, following cursor.next, until it
// finds the message; an idle message (OpenCode appends one when an execution
// ends) seen before it is newer, so that execution ended after the message
// was delivered. When the message is not in the history, the inbox is read
// and, if it is not pending there, the history is scanned again, as in
// QuerySteerDelivery. No read changes the session.
func (c *cliControl) QueryReleasedSteer(ctx context.Context, query SteerDeliveryQuery) (ReleasedSteerObservation, error) {
	found, idleAfter, err := c.scanHistoryForMessage(ctx, query)
	if err != nil || found {
		return ReleasedSteerObservation{InHistory: found, IdleAfterMessage: idleAfter}, err
	}
	pending, err := c.queryMessageInInbox(ctx, query)
	if err != nil || pending {
		return ReleasedSteerObservation{InInbox: pending}, err
	}
	found, idleAfter, err = c.scanHistoryForMessage(ctx, query)
	if err != nil {
		return ReleasedSteerObservation{}, err
	}
	return ReleasedSteerObservation{InHistory: found, IdleAfterMessage: idleAfter}, nil
}

// scanHistoryForMessage pages the history newest first. It reports whether
// the message is present and whether an idle message is newer than it.
//
// Ordering evidence (OpenCode 2.0.22 binary, SessionStore.messages): the
// listing orders by the server-assigned session_message.seq, the append
// sequence of the history, never by message ID, so an adapter-chosen message
// ID cannot reorder it. The cursor encodes the order of the first page and
// continues strictly below (desc) the cursor message's seq; the server
// rejects an order parameter combined with a cursor ("Cursor cannot be
// combined with order"), so order=desc is sent only on the first page.
func (c *cliControl) scanHistoryForMessage(ctx context.Context, query SteerDeliveryQuery) (bool, bool, error) {
	idleSeen := false
	cursor := ""
	for page := 0; page < releasedHistoryMaxPages; page++ {
		args := []string{"api", "session.message.list", "--param", "sessionID=" + string(query.SessionID), "--param", "limit=" + strconv.Itoa(releasedHistoryPageSize)}
		if cursor == "" {
			args = append(args, "--param", "order=desc")
		} else {
			args = append(args, "--param", "cursor="+cursor)
		}
		out, err := c.read(ctx, query.Directory, args...)
		if err != nil {
			return false, false, err
		}
		var response struct {
			Data *[]struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
			Cursor struct {
				Next *string `json:"next"`
			} `json:"cursor"`
		}
		if !out.succeeded || json.Unmarshal(out.stdout, &response) != nil || response.Data == nil {
			return false, false, errors.New("OpenCode session.message.list failed")
		}
		for _, message := range *response.Data {
			if message.ID == query.MessageID {
				return true, idleSeen, nil
			}
			if message.Type == "idle" {
				idleSeen = true
			}
		}
		if len(*response.Data) == 0 || response.Cursor.Next == nil || *response.Cursor.Next == "" {
			return false, false, nil
		}
		cursor = *response.Cursor.Next
	}
	return false, false, errors.New("OpenCode session history exceeds the scan bound")
}

func (c *cliControl) queryMessageInHistory(ctx context.Context, query SteerDeliveryQuery) (bool, error) {
	out, err := c.read(ctx, query.Directory, "api", "session.message.get", "--param", "sessionID="+string(query.SessionID), "--param", "messageID="+query.MessageID)
	if err != nil {
		return false, err
	}
	if out.succeeded {
		var response struct {
			Data *struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(out.stdout, &response) != nil || response.Data == nil || response.Data.ID != query.MessageID {
			return false, errors.New("invalid session.message.get response")
		}
		return true, nil
	}
	var failure struct {
		Tag       string `json:"_tag"`
		MessageID string `json:"messageID"`
	}
	if json.Unmarshal(out.stdout, &failure) != nil || failure.Tag != "MessageNotFoundError" || failure.MessageID != query.MessageID {
		return false, errors.New("OpenCode session.message.get failed")
	}
	return false, nil
}

func (c *cliControl) queryMessageInInbox(ctx context.Context, query SteerDeliveryQuery) (bool, error) {
	out, err := c.read(ctx, query.Directory, "api", "session.inbox.list", "--param", "sessionID="+string(query.SessionID))
	if err != nil {
		return false, err
	}
	var response struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if !out.succeeded || json.Unmarshal(out.stdout, &response) != nil || response.Data == nil {
		return false, errors.New("OpenCode session.inbox.list failed")
	}
	for _, item := range *response.Data {
		if item.ID == query.MessageID {
			return true, nil
		}
	}
	return false, nil
}

// commandOutput is a completed control command. succeeded is false for a
// nonzero exit; stdout still holds the error body `opencode api` printed.
type commandOutput struct {
	stdout    []byte
	succeeded bool
}

// read runs a read-only control command. It fails only when the command
// could not run to completion; a nonzero exit is reported in the output.
func (c *cliControl) read(ctx context.Context, dir string, args ...string) (commandOutput, error) {
	out, _, err := c.run(ctx, dir, args...)
	return out, err
}

func (c *cliControl) call(ctx context.Context, dir string, args ...string) ([]byte, bool, error) {
	out, sent, err := c.run(ctx, dir, args...)
	if err != nil {
		return nil, sent, err
	}
	if !out.succeeded {
		return nil, true, errors.New("OpenCode control command failed")
	}
	return out.stdout, true, nil
}

// run starts one owned control command, drains both pipes and waits for it.
// sent is false only when the command never started.
func (c *cliControl) run(ctx context.Context, dir string, args ...string) (commandOutput, bool, error) {
	if err := ctx.Err(); err != nil {
		return commandOutput{}, false, err
	}
	proc, err := c.launcher.Start(processruntime.Spec{Path: c.executable, Args: args, Dir: dir})
	if err != nil {
		return commandOutput{}, false, err
	}
	var stdout, stderr limitedBuffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&stdout, proc.Stdout()) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&stderr, proc.Stderr()) }()
	done := make(chan processruntime.Result, 1)
	go func() { result := proc.Wait(); wg.Wait(); done <- result }()
	select {
	case <-ctx.Done():
		_ = proc.KillTree()
		return commandOutput{}, true, ctx.Err()
	case result := <-done:
		return commandOutput{stdout: stdout.Bytes(), succeeded: result.Err == nil && result.ExitCode == 0}, true, nil
	}
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(value []byte) (int, error) {
	const limit = 1024 * 1024
	n := len(value)
	if b.Len() < limit {
		keep := limit - b.Len()
		if keep > n {
			keep = n
		}
		_, _ = b.Buffer.Write(value[:keep])
	}
	return n, nil
}
