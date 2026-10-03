package live

import "testing"

func TestGate(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		allowed bool
	}{
		{"absent", nil, false},
		{"codex", map[string]string{"CONDUIT_LIVE_HARNESS": "one-approved-turn", "CONDUIT_LIVE_PROVIDER": "codex", "CONDUIT_LIVE_MODEL_VERIFIED": "gpt-5.6-luna"}, true},
		{"opencode", map[string]string{"CONDUIT_LIVE_HARNESS": "one-approved-turn", "CONDUIT_LIVE_PROVIDER": "opencode", "CONDUIT_LIVE_MODEL_VERIFIED": "opencode/mimo-v2.6-flash-free"}, true},
		{"wrong-model", map[string]string{"CONDUIT_LIVE_HARNESS": "one-approved-turn", "CONDUIT_LIVE_PROVIDER": "codex", "CONDUIT_LIVE_MODEL_VERIFIED": "gpt-6.1-sol"}, false},
		{"missing-model", map[string]string{"CONDUIT_LIVE_HARNESS": "one-approved-turn", "CONDUIT_LIVE_PROVIDER": "codex"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CheckGate(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if (err == nil) != tc.allowed {
				t.Fatal("unexpected gate outcome")
			}
		})
	}
}

func TestCIPresenceAlwaysRefusesBeforeOptIn(t *testing.T) {
	for _, key := range []string{"CI", "GITHUB_ACTIONS"} {
		for _, value := range []string{"", "false", "true"} {
			t.Run(key+"="+value, func(t *testing.T) {
				_, err := CheckGate(func(k string) (string, bool) {
					if k == "CI" || k == "GITHUB_ACTIONS" {
						return value, k == key
					}
					t.Fatal("gate read opt-in after CI presence")
					return "", false
				})
				if err == nil {
					t.Fatal("CI allowed")
				}
			})
		}
	}
}
