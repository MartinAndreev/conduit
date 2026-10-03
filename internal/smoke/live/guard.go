// Package live contains manual-only live harness smoke tooling. Ordinary builds
// contain only the deterministic gate; the runner requires the liveharness tag.
package live

import "errors"

var ErrDisabled = errors.New("manual live harness gate refused execution")

// CheckGate reads only the named environment flags. CI presence takes priority,
// including an empty value or the string false. It never starts a process.
func CheckGate(lookup func(string) (string, bool)) (string, error) {
	for _, key := range []string{"CI", "GITHUB_ACTIONS"} {
		if _, present := lookup(key); present {
			return "", ErrDisabled
		}
	}
	if value, _ := lookup("CONDUIT_LIVE_HARNESS"); value != "one-approved-turn" {
		return "", ErrDisabled
	}
	selected, _ := lookup("CONDUIT_LIVE_PROVIDER")
	model, _ := lookup("CONDUIT_LIVE_MODEL_VERIFIED")
	switch selected {
	case "codex":
		if model == "gpt-5.6-luna" {
			return selected, nil
		}
	case "opencode":
		if model == "opencode/mimo-v2.6-flash-free" {
			return selected, nil
		}
	}
	return "", ErrDisabled
}
