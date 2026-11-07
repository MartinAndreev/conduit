package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func selectOption(id, category, current string, values ...string) map[string]any {
	options := make([]any, 0, len(values))
	for _, value := range values {
		options = append(options, map[string]any{"value": value, "name": value})
	}
	return map[string]any{"id": id, "name": id, "category": category, "type": "select", "currentValue": current, "options": options}
}

// fxShapedOptions mirrors the recorded fx session/new config options.
func fxShapedOptions() []map[string]any {
	return []map[string]any{
		selectOption("provider", "model", "gateway", "gateway", "codex"),
		selectOption("model", "model", "vendor/model-a", "vendor/model-a", "vendor/model-b"),
		selectOption("effort", "thought_level", "auto", "auto", "none", "low", "medium", "high", "xhigh"),
		selectOption("mode", "mode", "ask", "auto", "ask"),
	}
}

// statefulConfigAgent answers set_config_option by updating currentValue and
// returning every option, like fx does. reject names a config ID to refuse.
func statefulConfigAgent(options []map[string]any, reject string) func(*agentConn) {
	listed := make([]any, len(options))
	for index, option := range options {
		listed[index] = option
	}
	return standardAgent(nil, map[string]any{"sessionId": testSessionID, "configOptions": listed}, func(conn *agentConn, frame clientFrame) {
		if frame.Method != "session/set_config_option" {
			return
		}
		var params struct {
			ConfigID string `json:"configId"`
			Value    string `json:"value"`
		}
		_ = json.Unmarshal(frame.Params, &params)
		if params.ConfigID == reject {
			conn.replyError(frame.ID, -32602, "Invalid params", nil)
			return
		}
		for _, option := range options {
			if option["id"] == params.ConfigID {
				option["currentValue"] = params.Value
			}
		}
		conn.reply(frame.ID, map[string]any{"configOptions": listed})
	})
}

func openSelecting(t *testing.T, descriptor Descriptor, model provider.ModelSelection, serve func(*agentConn)) (*fakeLauncher, error) {
	t.Helper()
	launcher := &fakeLauncher{serve: serve}
	adapter := New(descriptor, Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	request := openRequest(t.TempDir())
	request.Model = model
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opened, err := adapter.OpenSession(ctx, request)
	if err == nil {
		t.Cleanup(func() {
			_, _ = opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
		})
	}
	return launcher, err
}

func mappedDescriptor() Descriptor {
	descriptor := testDescriptor()
	descriptor.ProviderConfigID = "provider"
	descriptor.VariantConfigID = "effort"
	return descriptor
}

func setCalls(launcher *fakeLauncher) []string {
	var calls []string
	for _, frame := range launcher.framesFor("session/set_config_option") {
		var params struct {
			ConfigID string `json:"configId"`
			Value    string `json:"value"`
		}
		_ = json.Unmarshal(frame.Params, &params)
		calls = append(calls, params.ConfigID+"="+params.Value)
	}
	return calls
}

func TestSelectionAppliesProviderModelVariantThenSettingsInOrder(t *testing.T) {
	descriptor := mappedDescriptor()
	descriptor.SessionSettings = []ConfigSetting{{ConfigID: "mode", Value: "auto"}}
	launcher, err := openSelecting(t, descriptor,
		provider.ModelSelection{ProviderID: "codex", ModelID: "vendor/model-b", Variant: "high"},
		statefulConfigAgent(fxShapedOptions(), ""))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	want := "provider=codex,model=vendor/model-b,effort=high,mode=auto"
	if got := strings.Join(setCalls(launcher), ","); got != want {
		t.Fatalf("set calls = %s, want %s", got, want)
	}
}

func TestSelectionEmptyProviderAndVariantKeepDefaults(t *testing.T) {
	launcher, err := openSelecting(t, mappedDescriptor(), provider.ModelSelection{}, statefulConfigAgent(fxShapedOptions(), ""))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if calls := setCalls(launcher); len(calls) != 0 || len(launcher.framesFor("session/set_model")) != 0 {
		t.Fatalf("defaults were overridden: %v", calls)
	}
}

func TestSelectionAlreadyCurrentVariantSkipsCall(t *testing.T) {
	launcher, err := openSelecting(t, mappedDescriptor(), provider.ModelSelection{ProviderID: "gateway", Variant: "auto"}, statefulConfigAgent(fxShapedOptions(), ""))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if calls := setCalls(launcher); len(calls) != 0 {
		t.Fatalf("current values were re-sent: %v", calls)
	}
}

func TestSelectionFailuresAreUnsupportedAndReaped(t *testing.T) {
	unmapped := testDescriptor()
	cases := []struct {
		name       string
		descriptor Descriptor
		model      provider.ModelSelection
		reject     string
		wantCalls  string
	}{
		{name: "provider not listed", descriptor: mappedDescriptor(), model: provider.ModelSelection{ProviderID: "other"}},
		{name: "provider without mapping", descriptor: unmapped, model: provider.ModelSelection{ProviderID: "codex"}},
		{name: "provider rejected", descriptor: mappedDescriptor(), model: provider.ModelSelection{ProviderID: "codex"}, reject: "provider", wantCalls: "provider=codex"},
		{name: "variant not listed", descriptor: mappedDescriptor(), model: provider.ModelSelection{ModelID: "vendor/model-b", Variant: "extreme"}, wantCalls: "model=vendor/model-b"},
		{name: "variant without mapping", descriptor: unmapped, model: provider.ModelSelection{Variant: "high"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			launcher, err := openSelecting(t, tc.descriptor, tc.model, statefulConfigAgent(fxShapedOptions(), tc.reject))
			requireCode(t, err, provider.ErrorCapabilityUnsupported)
			var nativeErr *rpcError
			if errors.As(err, &nativeErr) != (tc.reject != "") {
				t.Fatalf("native cause preserved = %v (%v)", tc.reject == "", err)
			}
			if got := strings.Join(setCalls(launcher), ","); got != tc.wantCalls {
				t.Fatalf("set calls = %q, want %q", got, tc.wantCalls)
			}
			if !launcher.process(t).reaped() {
				t.Fatal("failed open left the agent running")
			}
		})
	}
}
