package opencode

import (
	"bytes"
	"encoding/json"
	"strings"
)

// redactionMarker replaces the session's server password in every text the
// adapter emits.
const redactionMarker = "[conduit-redacted]"

// redactSecret replaces every exact occurrence of secret in text with the
// marker. An empty secret redacts nothing. It cannot recognize a partial
// value (for example output OpenCode itself cut inside the password) or an
// encoded form of it.
func redactSecret(text, secret string) string {
	if secret == "" || !strings.Contains(text, secret) {
		return text
	}
	return strings.ReplaceAll(text, secret, redactionMarker)
}

// redact removes the session's own server password from native text before
// it becomes an event. Tools executed by the server inherit
// OPENCODE_SERVER_PASSWORD (verified live, OpenCode 2.0.22), so a command
// such as `env` can put it into tool output.
func (s *session) redact(text string) string {
	if s.endpoint.password == nil {
		return text
	}
	return redactSecret(text, s.endpoint.password())
}

// redactJSON redacts secret from every decoded string (values and keys) of a
// JSON value, so a JSON-escaped spelling of it is caught too. The value is
// re-serialized only when something was redacted; otherwise raw is returned
// unchanged. Unparseable input falls back to redacting the raw text.
func redactJSON(raw json.RawMessage, secret string) json.RawMessage {
	if secret == "" || len(raw) == 0 {
		return raw
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return json.RawMessage(redactSecret(string(raw), secret))
	}
	redacted, changed := redactValue(value, secret)
	if !changed {
		return raw
	}
	out, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage(redactSecret(string(raw), secret))
	}
	return out
}

func redactValue(value any, secret string) (any, bool) {
	switch typed := value.(type) {
	case string:
		redacted := redactSecret(typed, secret)
		return redacted, redacted != typed
	case []any:
		changed := false
		for i, item := range typed {
			var itemChanged bool
			typed[i], itemChanged = redactValue(item, secret)
			changed = changed || itemChanged
		}
		return typed, changed
	case map[string]any:
		out := make(map[string]any, len(typed))
		changed := false
		for key, item := range typed {
			redactedKey := redactSecret(key, secret)
			redactedItem, itemChanged := redactValue(item, secret)
			out[redactedKey] = redactedItem
			changed = changed || itemChanged || redactedKey != key
		}
		return out, changed
	}
	return value, false
}

// redactEvent redacts the session password from every decoded field of a
// native event before any of it reaches an event: identifiers, kinds, texts,
// and the tool input and error, walked as JSON.
func (s *session) redactEvent(event *nativeEvent) {
	if s.endpoint.password == nil {
		return
	}
	secret := s.endpoint.password()
	for _, field := range []*string{&event.Type, &event.SessionID, &event.Part.ID, &event.Part.Type, &event.Part.Text, &event.Part.Tool, &event.Part.State.Status, &event.Part.State.Output, &event.Part.State.Title} {
		*field = redactSecret(*field, secret)
	}
	event.Part.State.Input = redactJSON(event.Part.State.Input, secret)
	event.Part.State.Error = redactJSON(event.Part.State.Error, secret)
}
