package configuration

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func fixture() string {
	return "schema_version: 1\nharnesses:\n  codex: {}\n  opencode: {}\nassignments: {}\n"
}
func fixtureV2() string {
	return "schema_version: 2\nharnesses:\n  codex: {}\n  opencode: {}\n  claude: {}\n  fx: {}\nassignments: {}\n"
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
	if _, err := Decode([]byte(strings.Replace(fixture(), "schema_version: 1", "schema_version: 3", 1))); !errors.Is(err, ErrFutureSchema) {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(fixture())); err != nil {
		t.Fatal(err)
	}
}
func TestInitialRoundTripNoAssignments(t *testing.T) {
	observations := map[HarnessID]Observation{Codex: {Kind: Found, Path: filepath.Join(t.TempDir(), "codex"), Version: "0.159.3"}, OpenCode: {Kind: Missing}, Claude: {Kind: Found, Path: filepath.Join(t.TempDir(), "claude"), Version: "2.1.316"}, FX: {Kind: Found, Path: filepath.Join(t.TempDir(), "fx")}}
	data, err := Encode(Initial(observations))
	if err != nil {
		t.Fatal(err)
	}
	config, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if config.SchemaVersion != CurrentSchema || len(config.Harnesses) != len(Supported()) || len(config.Assignments) != 0 || config.Harnesses[Codex].InitialDiscovery.Version != "0.159.3" {
		t.Fatal(config)
	}
}

func TestFutureSchemaWithNewFieldsIsDistinct(t *testing.T) {
	_, err := Decode([]byte("schema_version: 3\nnew_future_feature: value\n"))
	if !errors.Is(err, ErrFutureSchema) {
		t.Fatal(err)
	}
}

func TestFutureSchemaSkipsVersionOneScalarConstraints(t *testing.T) {
	for _, value := range []string{"true", "123", "null"} {
		t.Run(value, func(t *testing.T) {
			_, err := Decode([]byte("schema_version: 3\nnew_future_feature: " + value + "\n"))
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
		"duplicate schema":   "schema_version: 3\nschema_version: 3\n",
		"duplicate field":    "schema_version: 3\nfuture: true\nfuture: false\n",
		"multiple documents": "schema_version: 3\n---\nschema_version: 3\n",
		"alias":              "schema_version: 3\nfuture: &future true\nother: *future\n",
		"quoted version":     "schema_version: '3'\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(input)); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
}

func TestSupportedHarnesses(t *testing.T) {
	want := []HarnessID{Codex, OpenCode, Claude, FX}
	if !slices.Equal(Supported(), want) || Claude != "claude" || FX != "fx" {
		t.Fatal(Supported())
	}
}

func TestSchemaV2Validation(t *testing.T) {
	for name, input := range map[string]string{
		"all supported":    fixtureV2(),
		"missing harness":  strings.Replace(fixtureV2(), "  fx: {}\n", "", 1),
		"no harnesses yet": "schema_version: 2\nharnesses: {}\nassignments: {}\n",
		"v1":               fixture(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(input)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, input := range map[string]string{
		"unknown id":           strings.Replace(fixtureV2(), "fx: {}", "grok: {}", 1),
		"v1 with claude":       strings.Replace(fixture(), "opencode: {}", "opencode: {}\n  claude: {}", 1),
		"v1 missing opencode":  strings.Replace(fixture(), "  opencode: {}\n", "", 1),
		"relative executable":  strings.Replace(fixtureV2(), "claude: {}", "claude: {executable: claude}", 1),
		"bad discovery kind":   strings.Replace(fixtureV2(), "fx: {}", "fx: {initial_discovery: {kind: maybe}}", 1),
		"version without find": strings.Replace(fixtureV2(), "fx: {}", "fx: {initial_discovery: {kind: probe_failed, path: "+strconv.Quote(filepath.Join(t.TempDir(), "fx"))+", version: 1.0.0}}", 1),
		"assignments":          strings.Replace(fixtureV2(), "assignments: {}", "assignments: {writer: model}", 1),
		"no harnesses key":     "schema_version: 2\nassignments: {}\n",
		"version zero":         strings.Replace(fixtureV2(), "schema_version: 2", "schema_version: 0", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(input)); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	if _, err := Decode([]byte(strings.Replace(fixtureV2(), "schema_version: 2", "schema_version: 3", 1))); !errors.Is(err, ErrFutureSchema) {
		t.Fatal(err)
	}
}

func TestEncodeWritesOnlyCurrentSchema(t *testing.T) {
	v1, err := Decode([]byte(fixture()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Encode(v1); !errors.Is(err, ErrInvalid) {
		t.Fatal("encoded v1", err)
	}
	partial := Config{SchemaVersion: CurrentSchema, Harnesses: map[HarnessID]Harness{Claude: {}}, Assignments: map[string]string{}}
	data, err := Encode(partial)
	if err != nil || !strings.Contains(string(data), "schema_version: 2") {
		t.Fatal(string(data), err)
	}
}

func TestMigrateV1UsesDiscoveryOnlyForMissingHarnesses(t *testing.T) {
	codex := strconv.Quote(filepath.Join(t.TempDir(), "codex"))
	input := strings.Replace(fixture(), "codex: {}", "codex: {executable: "+codex+", initial_discovery: {kind: found, path: "+codex+", version: 0.1.0}}", 1)
	v1, err := Decode([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	discovered := map[HarnessID]Observation{
		Codex:  {Kind: Found, Path: filepath.Join(t.TempDir(), "new-codex"), Version: "9.9.9"},
		Claude: {Kind: Found, Path: filepath.Join(t.TempDir(), "claude"), Version: "2.1.316"},
		FX:     {Kind: Missing},
	}
	if got := MissingHarnesses(v1); !slices.Equal(got, []HarnessID{Claude, FX}) {
		t.Fatal(got)
	}
	next, err := MigrateV1(v1, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if next.SchemaVersion != CurrentSchema || len(next.Harnesses) != len(Supported()) {
		t.Fatal(next)
	}
	if !reflect.DeepEqual(next.Harnesses[Codex], v1.Harnesses[Codex]) || !reflect.DeepEqual(next.Harnesses[OpenCode], v1.Harnesses[OpenCode]) {
		t.Fatal("existing entries changed", next.Harnesses)
	}
	if *next.Harnesses[Claude].InitialDiscovery != discovered[Claude] || *next.Harnesses[FX].InitialDiscovery != discovered[FX] {
		t.Fatal(next.Harnesses)
	}
	if _, err := Encode(next); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateV1(v1, map[HarnessID]Observation{Claude: {Kind: Missing}}); !errors.Is(err, ErrInvalid) {
		t.Fatal("migrated without discovery for fx", err)
	}
	current, _ := Decode([]byte(fixtureV2()))
	if _, err := MigrateV1(current, discovered); !errors.Is(err, ErrInvalid) {
		t.Fatal("migrated a v2 file", err)
	}
}
