package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

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
	for _, action := range []string{"external_directory", "webfetch", "websearch"} {
		payload.Permissions = append(payload.Permissions, struct {
			Action   string `json:"action"`
			Resource string `json:"resource"`
			Effect   string `json:"effect"`
		}{Action: action, Resource: "*", Effect: "deny"})
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
		Text     string `json:"text"`
		Delivery string `json:"delivery"`
	}{request.Text, request.Delivery})
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

func (c *cliControl) call(ctx context.Context, dir string, args ...string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	proc, err := c.launcher.Start(processruntime.Spec{Path: c.executable, Args: args, Dir: dir})
	if err != nil {
		return nil, false, err
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
		return nil, true, ctx.Err()
	case result := <-done:
		if result.Err != nil || result.ExitCode != 0 {
			return nil, true, errors.New("OpenCode control command failed")
		}
		return stdout.Bytes(), true, nil
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
