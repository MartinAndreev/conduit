// Package configuration owns the startup configuration schema, not current readiness.
package configuration

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type HarnessID string

const (
	Codex    HarnessID = "codex"
	OpenCode HarnessID = "opencode"
)

func Supported() []HarnessID { return []HarnessID{Codex, OpenCode} }

type DiscoveryKind string

const (
	Missing      DiscoveryKind = "missing"
	Found        DiscoveryKind = "found"
	ProbeFailed  DiscoveryKind = "probe_failed"
	ProbeTimeout DiscoveryKind = "probe_timeout"
)

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
					if err := value.Decode(&version); err == nil && version > 1 {
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
	if c.SchemaVersion > 1 {
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

// v1 rejects scalar coercion only after structural validation and version
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

func Validate(c Config) error {
	if c.SchemaVersion != 1 || len(c.Harnesses) != len(Supported()) || c.Assignments == nil || len(c.Assignments) != 0 {
		return ErrInvalid
	}
	for _, id := range Supported() {
		h, ok := c.Harnesses[id]
		if !ok {
			return ErrInvalid
		}
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
	}
	return nil
}
func Encode(c Config) ([]byte, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	return yaml.Marshal(c)
}
func Initial(observations map[HarnessID]Observation) Config {
	c := Config{SchemaVersion: 1, Harnesses: map[HarnessID]Harness{}, Assignments: map[string]string{}}
	for _, id := range Supported() {
		o := observations[id]
		c.Harnesses[id] = Harness{InitialDiscovery: &o}
	}
	return c
}
