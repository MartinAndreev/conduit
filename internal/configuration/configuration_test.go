package configuration

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func fixture() string {
	return "schema_version: 1\nharnesses:\n  codex: {}\n  opencode: {}\nassignments: {}\n"
}
func TestStrictDecode(t *testing.T) {
	cases := []string{
		strings.Replace(fixture(), "schema_version: 1", "schema_version: '1'", 1),
		"schema_version: 1\n" + fixture(),
		fixture() + "credentials: secret\n",
		strings.Replace(fixture(), "codex: {}", "codex: {unknown: secret}", 1),
		strings.Replace(fixture(), "codex: {}", "codex: {executable: 123}", 1),
		strings.Replace(fixture(), "codex: {}", "codex: {executable: relative/path}", 1),
		strings.Replace(fixture(), "assignments: {}", "assignments: {writer: paid-model}", 1),
		fixture() + "---\n" + fixture(),
		strings.Replace(fixture(), "opencode: {}", "opencode: &alias {}", 1) + "other: *alias\n",
		strings.Replace(fixture(), "opencode: {}", "opencode: {initial_discovery: {kind: missing, path: /secret}}", 1),
	}
	for i, input := range cases {
		if _, err := Decode([]byte(input)); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if _, err := Decode([]byte(strings.Replace(fixture(), "schema_version: 1", "schema_version: 2", 1))); !errors.Is(err, ErrFutureSchema) {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(fixture())); err != nil {
		t.Fatal(err)
	}
}
func TestInitialRoundTripNoAssignments(t *testing.T) {
	observations := map[HarnessID]Observation{Codex: {Kind: Found, Path: filepath.Join(t.TempDir(), "codex"), Version: "0.159.3"}, OpenCode: {Kind: Missing}}
	data, err := Encode(Initial(observations))
	if err != nil {
		t.Fatal(err)
	}
	config, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Assignments) != 0 || config.Harnesses[Codex].InitialDiscovery.Version != "0.159.3" {
		t.Fatal(config)
	}
}

func TestFutureSchemaWithNewFieldsIsDistinct(t *testing.T) {
	_, err := Decode([]byte("schema_version: 2\nnew_future_feature: value\n"))
	if !errors.Is(err, ErrFutureSchema) {
		t.Fatal(err)
	}
}

func TestFutureSchemaSkipsVersionOneScalarConstraints(t *testing.T) {
	for _, value := range []string{"true", "123", "null"} {
		t.Run(value, func(t *testing.T) {
			_, err := Decode([]byte("schema_version: 2\nnew_future_feature: " + value + "\n"))
			if !errors.Is(err, ErrFutureSchema) {
				t.Fatalf("future field %s: %v", value, err)
			}
			input := strings.Replace(fixture(), "codex: {}", "codex: {executable: "+value+"}", 1)
			if _, err := Decode([]byte(input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("v1 scalar %s: %v", value, err)
			}
		})
	}
}

func TestFutureSchemaStillRejectsUnsafeStructure(t *testing.T) {
	for name, input := range map[string]string{
		"duplicate schema":   "schema_version: 2\nschema_version: 2\n",
		"duplicate field":    "schema_version: 2\nfuture: true\nfuture: false\n",
		"multiple documents": "schema_version: 2\n---\nschema_version: 2\n",
		"alias":              "schema_version: 2\nfuture: &future true\nother: *future\n",
		"quoted version":     "schema_version: '2'\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(input)); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
}
