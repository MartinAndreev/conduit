package execution

import (
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func manifestFacts(c compose.Composition) ManifestFacts {
	return ManifestFacts{Assignment: assignment(3), Request: composedRequest(), Composition: c, MaxPayloadBytes: MaxManifestPayloadBytes}
}

func TestDecideManifestEnvelopeRefusals(t *testing.T) {
	text := "memory block"
	for _, tc := range []struct {
		name   string
		change func(*ManifestFacts)
		want   ManifestEnvelopeKind
	}{
		{"no limit", func(f *ManifestFacts) { f.MaxPayloadBytes = 0 }, ManifestInvalidFacts},
		{"no project", func(f *ManifestFacts) { f.Assignment.ProjectID = "" }, ManifestInvalidFacts},
		{"invalid request", func(f *ManifestFacts) { f.Composition = compose.Composition{Kind: compose.InvalidRequest} }, ManifestKindNotDispatchable},
		{"blocked", func(f *ManifestFacts) { f.Composition.Kind = compose.BlockedExplicitNotFound }, ManifestKindNotDispatchable},
		{"unknown search status", func(f *ManifestFacts) { f.Composition.Search.Status = "" }, ManifestInvalidFacts},
		{"other project", func(f *ManifestFacts) { f.Request.ProjectID = "other" }, ManifestScopeMismatch},
		{"other attempt", func(f *ManifestFacts) { f.Request.AttemptID = "other" }, ManifestScopeMismatch},
		{"instruction part", func(f *ManifestFacts) { f.Composition.Part.Kind = provider.InputInstruction }, ManifestPartInvalid},
		{"part with URI", func(f *ManifestFacts) { f.Composition.Part.URI = "file:///x" }, ManifestPartInvalid},
		{"part with media type", func(f *ManifestFacts) { f.Composition.Part.MediaType = "text/plain" }, ManifestPartInvalid},
		{"empty text", func(f *ManifestFacts) { f.Composition.Part.Text, f.Composition.TextHash = "", EmptyPayloadHash }, ManifestPartInvalid},
		{"hash of other text", func(f *ManifestFacts) { f.Composition.TextHash = PayloadHash("other") }, ManifestHashMismatch},
		{"no items", func(f *ManifestFacts) { f.Composition.Items, f.Composition.Expected = nil, nil }, ManifestCoverageMismatch},
		{"missing expected", func(f *ManifestFacts) { f.Composition.Expected = f.Composition.Expected[:1] }, ManifestCoverageMismatch},
		{"extra expected", func(f *ManifestFacts) {
			f.Composition.Expected = append(f.Composition.Expected, memory.ExpectedRecordVersion{RecordID: "r3", VersionID: "r3-v1", RecordVersion: 1})
		}, ManifestCoverageMismatch},
		{"expected for other version", func(f *ManifestFacts) { f.Composition.Expected[1].VersionID = "r2-v2" }, ManifestCoverageMismatch},
		{"duplicate item", func(f *ManifestFacts) {
			f.Composition.Items[1] = f.Composition.Items[0]
			f.Composition.Expected[1] = f.Composition.Expected[0]
		}, ManifestCoverageMismatch},
		{"item without version", func(f *ManifestFacts) {
			f.Composition.Items[0].VersionID, f.Composition.Expected[0].VersionID = "", ""
		}, ManifestCoverageMismatch},
		{"different policies", func(f *ManifestFacts) { f.Composition.Items[1].PolicyVersion = "2" }, ManifestPolicyMismatch},
		{"empty policy", func(f *ManifestFacts) {
			f.Composition.Items[0].PolicyVersion, f.Composition.Items[1].PolicyVersion = "", ""
		}, ManifestPolicyMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := manifestFacts(composedComposition(text))
			tc.change(&facts)
			got := DecideManifestEnvelope(facts)
			if got.Kind != tc.want || got.Reason == "" || got.Manifest.ManifestHash != "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideManifestEnvelopeComposed(t *testing.T) {
	text := "memory block"
	got := DecideManifestEnvelope(manifestFacts(composedComposition(text)))
	m := got.Manifest
	if got.Kind != ManifestEnvelope || m.Composition != CompositionComposed || m.Payload != text || m.PayloadHash != PayloadHash(text) ||
		m.RedactionPolicyVersion != "1" || m.SearchStatus != compose.SearchOK || len(m.Items) != 2 || len(m.Exclusions) != 1 ||
		len(m.Expected) != 2 || len(m.ManifestHash) != 64 || m.ManifestHash != ManifestHash(m) {
		t.Fatalf("decision = %+v", got)
	}
	if m.Items[1] != (ManifestItem{RecordID: "r2", VersionID: "r2-v3", Mode: "referenced", Reason: "found by search", Trust: memory.TrustConduitObserved}) {
		t.Fatalf("item = %+v", m.Items[1])
	}
}

// The limit is measured on the exact bytes stored and sent, including
// multi-byte UTF-8.
func TestDecideManifestEnvelopePayloadLimit(t *testing.T) {
	letter := "щ" // two bytes
	exact := strings.Repeat(letter, MaxManifestPayloadBytes/2)
	if len(exact) != MaxManifestPayloadBytes {
		t.Fatalf("fixture has %d bytes", len(exact))
	}
	if got := DecideManifestEnvelope(manifestFacts(composedComposition(exact))); got.Kind != ManifestEnvelope {
		t.Fatalf("%d bytes = %+v", len(exact), got.Kind)
	}
	over := exact[:len(exact)-2] + "€" // a three-byte letter crosses the limit
	if len(over) != MaxManifestPayloadBytes+1 {
		t.Fatalf("fixture has %d bytes", len(over))
	}
	if got := DecideManifestEnvelope(manifestFacts(composedComposition(over))); got.Kind != ManifestPayloadTooLarge {
		t.Fatalf("%d bytes = %+v", len(over), got.Kind)
	}
}

// The real Composer's empty DTO has no part, hash, items or expected
// versions; it becomes an empty manifest with the empty string's hash and no
// policy.
func TestDecideManifestEnvelopeRealEmptyComposition(t *testing.T) {
	composition := realEmptyComposition(t)
	got := DecideManifestEnvelope(manifestFacts(composition))
	m := got.Manifest
	if got.Kind != ManifestEnvelope || m.Composition != CompositionEmpty || m.PayloadHash != EmptyPayloadHash ||
		m.PayloadHash != PayloadHash("") || m.Payload != "" || m.RedactionPolicyVersion != "" ||
		m.SearchStatus != compose.SearchDisabled || len(m.Items) != 0 || m.ManifestHash != ManifestHash(m) {
		t.Fatalf("decision = %+v", got)
	}
	for name, change := range map[string]func(*compose.Composition){
		"part":     func(c *compose.Composition) { c.Part = provider.InputPart{Kind: provider.InputContext, Text: "x"} },
		"hash":     func(c *compose.Composition) { c.TextHash = EmptyPayloadHash },
		"items":    func(c *compose.Composition) { c.Items = composedComposition("x").Items },
		"expected": func(c *compose.Composition) { c.Expected = composedComposition("x").Expected },
	} {
		modified := composition
		change(&modified)
		if got := DecideManifestEnvelope(manifestFacts(modified)); got.Kind != ManifestEmptyNotEmpty {
			t.Fatalf("empty with %s = %+v", name, got.Kind)
		}
	}
}

// Every stored immutable field changes the manifest hash; the same data
// gives the same hash.
func TestManifestHashCoversEveryStoredField(t *testing.T) {
	base := DecideManifestEnvelope(manifestFacts(composedComposition("memory block"))).Manifest
	if again := DecideManifestEnvelope(manifestFacts(composedComposition("memory block"))).Manifest; again.ManifestHash != base.ManifestHash {
		t.Fatal("same data, different hash")
	}
	seen := map[string]string{base.ManifestHash: "base"}
	for name, change := range map[string]func(*NewManifest){
		"composition":         func(m *NewManifest) { m.Composition = CompositionEmpty },
		"payload hash":        func(m *NewManifest) { m.PayloadHash = PayloadHash("other") },
		"policy version":      func(m *NewManifest) { m.RedactionPolicyVersion = "2" },
		"policy NULL":         func(m *NewManifest) { m.RedactionPolicyVersion = "" },
		"search status":       func(m *NewManifest) { m.SearchStatus = compose.SearchDegraded },
		"item record":         func(m *NewManifest) { m.Items[0].RecordID = "r7" },
		"item version":        func(m *NewManifest) { m.Items[0].VersionID = "r1-v9" },
		"item mode":           func(m *NewManifest) { m.Items[0].Mode = "referenced" },
		"item reason":         func(m *NewManifest) { m.Items[0].Reason = "found by search" },
		"item trust":          func(m *NewManifest) { m.Items[0].Trust = memory.TrustAgentClaim },
		"item order":          func(m *NewManifest) { m.Items[0], m.Items[1] = m.Items[1], m.Items[0] },
		"no exclusions":       func(m *NewManifest) { m.Exclusions = nil },
		"exclusion record":    func(m *NewManifest) { m.Exclusions[0].RecordID = "r8" },
		"exclusion version":   func(m *NewManifest) { m.Exclusions[0].VersionID = "" },
		"exclusion withheld":  func(m *NewManifest) { m.Exclusions[0].IdentityWithheld = true },
		"exclusion mandatory": func(m *NewManifest) { m.Exclusions[0].Mandatory = true },
		"exclusion stage":     func(m *NewManifest) { m.Exclusions[0].Stage = compose.StageSize },
		"exclusion kind":      func(m *NewManifest) { m.Exclusions[0].Kind = "oversized" },
		"exclusion reason":    func(m *NewManifest) { m.Exclusions[0].Reason = "superseded" },
	} {
		m := base
		m.Items = append([]ManifestItem(nil), base.Items...)
		m.Exclusions = append([]ManifestExclusion(nil), base.Exclusions...)
		change(&m)
		hash := ManifestHash(m)
		if other, dup := seen[hash]; dup {
			t.Fatalf("%s gives the hash of %s", name, other)
		}
		seen[hash] = name
	}
	// Two empty compositions that differ only in their exclusions are
	// different manifests.
	first := realEmptyComposition(t)
	second := first
	second.Exclusions = []compose.Exclusion{{RecordID: "r1", Stage: compose.StageGather, Kind: compose.ExcludedNotFound, Reason: "missing"}}
	if DecideManifestEnvelope(manifestFacts(first)).Manifest.ManifestHash == DecideManifestEnvelope(manifestFacts(second)).Manifest.ManifestHash {
		t.Fatal("empty compositions with different exclusions share a hash")
	}
}

// The request hash covers every command field except the memory composition
// itself; composed and stored memory are the same presence.
func TestRequestHashCoversCommandIdentity(t *testing.T) {
	base := steerCommand()
	seen := map[string]string{RequestHash(base): "base"}
	if RequestHash(steerCommand()) != RequestHash(base) {
		t.Fatal("same command, different hash")
	}
	for name, change := range map[string]func(*Command){
		"kind":            func(c *Command) { c.Kind = IntentStartTurn },
		"session":         func(c *Command) { c.SessionID = "other" },
		"adapter session": func(c *Command) { c.Session.AdapterSessionID = "other" },
		"current":         func(c *Command) { c.Current.Revision = 2 },
		"new":             func(c *Command) { c.New.AssignmentID = "other" },
		"turn":            func(c *Command) { c.ExpectedTurn.AdapterTurnID = "turn-b" },
		"mode":            func(c *Command) { c.Mode = provider.DeliverNextBoundary },
		"contract name":   func(c *Command) { c.ResultContract.Name = "x" },
		"contract schema": func(c *Command) { c.ResultContract.Schema = []byte("{}") },
		"fallbacks": func(c *Command) {
			c.ResultContract.AllowedFallbacks = []provider.SchemaEnforcement{provider.SchemaNone}
		},
		"part text":  func(c *Command) { c.Parts[0].Text = "other" },
		"part kind":  func(c *Command) { c.Parts[0].Kind = provider.InputArtifactRef },
		"part uri":   func(c *Command) { c.Parts[0].URI = "file:///a" },
		"part media": func(c *Command) { c.Parts[0].MediaType = "text/plain" },
		"no parts":   func(c *Command) { c.Parts = nil },
		"memory":     func(c *Command) { c.Memory.Kind = MemoryInputComposed },
	} {
		c := steerCommand()
		change(&c)
		hash := RequestHash(c)
		if other, dup := seen[hash]; dup {
			t.Fatalf("%s gives the hash of %s", name, other)
		}
		seen[hash] = name
	}
	composed, stored := steerCommand(), steerCommand()
	composed.Memory = MemoryInput{Kind: MemoryInputComposed, Request: composedRequest(), Composition: composedComposition("a")}
	stored.Memory = MemoryInput{Kind: MemoryInputStored}
	if RequestHash(composed) != RequestHash(stored) {
		t.Fatal("composed and stored memory differ in the request hash")
	}
}

func TestValidateCommand(t *testing.T) {
	if err := ValidateCommand(startCommand()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCommand(steerCommand()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Command){
		"no intent id":       func(c *Command) { c.IntentID = "" },
		"long intent id":     func(c *Command) { c.IntentID = IntentID(fill(257)) },
		"unknown kind":       func(c *Command) { c.Kind = "interrupt" },
		"no session":         func(c *Command) { c.SessionID = "" },
		"no adapter session": func(c *Command) { c.Session.AdapterSessionID = "" },
		"zero revision":      func(c *Command) { c.Current.Revision = 0 },
		"revision overflow":  func(c *Command) { c.Current.Revision = MaxStoredRevision + 1 },
		"context part": func(c *Command) {
			c.Parts = append(c.Parts, provider.InputPart{Kind: provider.InputContext, Text: "x"})
		},
		"unknown memory":      func(c *Command) { c.Memory.Kind = "" },
		"stored with request": func(c *Command) { c.Memory = MemoryInput{Kind: MemoryInputStored, Request: composedRequest()} },
		"start with new":      func(c *Command) { c.New = assignment(4) },
		"start with turn":     func(c *Command) { c.ExpectedTurn.AdapterTurnID = "t" },
	} {
		c := startCommand()
		change(&c)
		if err := ValidateCommand(c); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	for name, change := range map[string]func(*Command){
		"no new":        func(c *Command) { c.New = provider.AssignmentRef{} },
		"no turn":       func(c *Command) { c.ExpectedTurn.AdapterTurnID = "" },
		"unknown mode":  func(c *Command) { c.Mode = "later" },
		"with contract": func(c *Command) { c.ResultContract.Name = "result" },
		"new overflow":  func(c *Command) { c.New.Revision = MaxStoredRevision + 1 },
	} {
		c := steerCommand()
		change(&c)
		if err := ValidateCommand(c); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
