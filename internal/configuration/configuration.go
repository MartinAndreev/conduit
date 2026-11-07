// Package configuration owns the startup configuration schema, not current readiness.
package configuration

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type HarnessID string

const (
	Codex    HarnessID = "codex"
	OpenCode HarnessID = "opencode"
	Claude   HarnessID = "claude"
	FX       HarnessID = "fx"
)

// Supported lists harnesses with implemented adapters. Consumers derive counts
// from it; new IDs are added here only when their adapter is ready.
func Supported() []HarnessID { return []HarnessID{Codex, OpenCode, Claude, FX} }

// SchemaV1 and CurrentSchema are the schema versions this build understands.
// Only v1 is accepted as a migration source; any version above CurrentSchema is
// ErrFutureSchema and is never rewritten.
const (
	SchemaV1      = 1
	CurrentSchema = 2
)

// schemaV1Harnesses is the frozen v1 membership: a v1 file has exactly these
// entries. It is schema history, not the supported list.
func schemaV1Harnesses() []HarnessID { return []HarnessID{Codex, OpenCode} }

type DiscoveryKind string

const (
	Missing      DiscoveryKind = "missing"
	Found        DiscoveryKind = "found"
	ProbeFailed  DiscoveryKind = "probe_failed"
	ProbeTimeout DiscoveryKind = "probe_timeout"
)

// Conclusive reports whether discovery established the harness's presence or
// absence. Probe failure and timeout are inconclusive.
func (k DiscoveryKind) Conclusive() bool { return k == Found || k == Missing }

type Observation struct {
	Kind    DiscoveryKind `yaml:"kind"`
	Path    string        `yaml:"path,omitempty"`
	Version string        `yaml:"version,omitempty"`
}
type Harness struct {
	Executable       string       `yaml:"executable,omitempty"`
	InitialDiscovery *Observation `yaml:"initial_discovery,omitempty"`
}
type Config struct {
	SchemaVersion int                   `yaml:"schema_version"`
	Harnesses     map[HarnessID]Harness `yaml:"harnesses"`
	Assignments   map[string]string     `yaml:"assignments"`
}

var ErrFutureSchema = errors.New("configuration schema is newer than supported")
var ErrInvalid = errors.New("invalid configuration")

// Decode rejects YAML coercions, aliases, multiple documents, and undeclared keys.
// No diagnostic includes source values: the file can contain accidental secrets.
func Decode(data []byte) (Config, error) {
	var node yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&node); err != nil {
		return Config{}, ErrInvalid
	}
	if err := strictNode(&node); err != nil {
		return Config{}, ErrInvalid
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return Config{}, ErrInvalid
	}
	// Future schemas are diagnosed without pretending to understand their fields.
	if len(node.Content) == 1 && node.Content[0].Kind == yaml.MappingNode {
		root := node.Content[0]
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == "schema_version" {
				var version int
				if value := root.Content[i+1]; value.Kind == yaml.ScalarNode && value.Tag == "!!int" {
					if err := value.Decode(&version); err == nil && version > CurrentSchema {
						return Config{}, ErrFutureSchema
					}
				}
			}
		}
	}
	if err := strictScalarTypes(&node); err != nil {
		return Config{}, ErrInvalid
	}
	var c Config
	d = yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return Config{}, ErrInvalid
	}
	if c.SchemaVersion > CurrentSchema {
		return Config{}, ErrFutureSchema
	}
	if err := Validate(c); err != nil {
		return Config{}, err
	}
	return c, nil
}
func strictNode(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return ErrInvalid
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
				return ErrInvalid
			}
			seen[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := strictNode(child); err != nil {
			return err
		}
	}
	return nil
}

// v1 and v2 reject scalar coercion only after structural validation and version
// recognition. Future schemas may define boolean, integer, or null fields.
func strictScalarTypes(n *yaml.Node) error {
	for i, child := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 1 && child.Kind == yaml.ScalarNode {
			key := n.Content[i-1].Value
			if key == "schema_version" {
				if child.Tag != "!!int" {
					return ErrInvalid
				}
			} else if child.Tag != "!!str" {
				return ErrInvalid
			}
		}
		if err := strictScalarTypes(child); err != nil {
			return err
		}
	}
	return nil
}

// Validate accepts a v1 file (exactly the v1 harness set, readable only as a
// migration source) or a v2 file. In v2 a missing supported harness means "not
// discovered yet"; an unknown harness ID is invalid. The remaining checks are
// shared by both versions.
func Validate(c Config) error {
	if c.Harnesses == nil || c.Assignments == nil || len(c.Assignments) != 0 {
		return ErrInvalid
	}
	switch c.SchemaVersion {
	case SchemaV1:
		if len(c.Harnesses) != len(schemaV1Harnesses()) {
			return ErrInvalid
		}
		for _, id := range schemaV1Harnesses() {
			if _, ok := c.Harnesses[id]; !ok {
				return ErrInvalid
			}
		}
	case CurrentSchema:
		for id := range c.Harnesses {
			if !slices.Contains(Supported(), id) {
				return ErrInvalid
			}
		}
	default:
		return ErrInvalid
	}
	for _, h := range c.Harnesses {
		if err := validateHarness(h); err != nil {
			return err
		}
	}
	return nil
}
func validateHarness(h Harness) error {
	if h.Executable != "" && (!filepath.IsAbs(h.Executable) || strings.ContainsAny(h.Executable, "\x00\r\n")) {
		return ErrInvalid
	}
	if o := h.InitialDiscovery; o != nil {
		switch o.Kind {
		case Missing:
			if o.Path != "" || o.Version != "" {
				return ErrInvalid
			}
		case Found, ProbeFailed, ProbeTimeout:
			if !filepath.IsAbs(o.Path) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if o.Version != "" && o.Kind != Found {
			return ErrInvalid
		}
	}
	return nil
}

// Encode writes only the current schema; a v1 value must be migrated first.
func Encode(c Config) ([]byte, error) {
	if c.SchemaVersion != CurrentSchema {
		return nil, ErrInvalid
	}
	if err := Validate(c); err != nil {
		return nil, err
	}
	return yaml.Marshal(c)
}
func Initial(observations map[HarnessID]Observation) Config {
	c := Config{SchemaVersion: CurrentSchema, Harnesses: map[HarnessID]Harness{}, Assignments: map[string]string{}}
	for _, id := range Supported() {
		o := observations[id]
		c.Harnesses[id] = Harness{InitialDiscovery: &o}
	}
	return c
}

// MissingHarnesses lists supported harnesses without an entry, in Supported order.
func MissingHarnesses(c Config) []HarnessID {
	var missing []HarnessID
	for _, id := range Supported() {
		if _, ok := c.Harnesses[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// MigrateV1 returns the v2 equivalent of a valid v1 file. Existing entries are
// copied unchanged; only harnesses without an entry receive an initial
// discovery observation, taken from discovered. It performs no I/O.
func MigrateV1(c Config, discovered map[HarnessID]Observation) (Config, error) {
	if c.SchemaVersion != SchemaV1 {
		return Config{}, ErrInvalid
	}
	if err := Validate(c); err != nil {
		return Config{}, err
	}
	next := Config{SchemaVersion: CurrentSchema, Harnesses: map[HarnessID]Harness{}, Assignments: map[string]string{}}
	for id, h := range c.Harnesses {
		if h.InitialDiscovery != nil {
			o := *h.InitialDiscovery
			h.InitialDiscovery = &o
		}
		next.Harnesses[id] = h
	}
	for _, id := range MissingHarnesses(c) {
		o, ok := discovered[id]
		if !ok {
			return Config{}, ErrInvalid
		}
		next.Harnesses[id] = Harness{InitialDiscovery: &o}
	}
	if err := Validate(next); err != nil {
		return Config{}, err
	}
	return next, nil
}
