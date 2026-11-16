package execution

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strconv"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// canonical writes values with their lengths in a fixed order, so no two
// different value lists encode to the same bytes.
type canonical struct{ h hash.Hash }

func newCanonical(domain string) canonical {
	c := canonical{h: sha256.New()}
	c.text(domain)
	return c
}

func (c canonical) bytes(value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	c.h.Write(length[:])
	c.h.Write(value)
}

func (c canonical) text(value string) { c.bytes([]byte(value)) }

func (c canonical) number(value uint64) { c.text(strconv.FormatUint(value, 10)) }

func (c canonical) flag(value bool) {
	if value {
		c.text("1")
		return
	}
	c.text("0")
}

func (c canonical) assignment(a provider.AssignmentRef) {
	c.text(string(a.ProjectID))
	c.text(string(a.RunID))
	c.text(string(a.AttemptID))
	c.text(string(a.AssignmentID))
	c.number(a.Revision)
}

func (c canonical) sum() string { return hex.EncodeToString(c.h.Sum(nil)) }

// RequestHash identifies a command's content: kind, session, both
// assignments, expected turn, delivery mode, result contract, the parts other
// than memory and whether memory is present. A repetition with the same
// intent ID must have the same hash; composed and stored memory both count as
// with_manifest.
func RequestHash(c Command) string {
	h := newCanonical("conduit.dispatch.request.v1")
	h.text(string(c.Kind))
	h.text(string(c.SessionID))
	h.text(string(c.Session.AdapterSessionID))
	h.assignment(c.Current)
	h.assignment(c.New)
	h.text(string(c.ExpectedTurn.AdapterTurnID))
	h.text(string(c.Mode))
	h.text(c.ResultContract.Name)
	h.text(c.ResultContract.Version)
	h.bytes(c.ResultContract.Schema)
	h.text(string(c.ResultContract.Requested))
	h.number(uint64(len(c.ResultContract.AllowedFallbacks)))
	for _, fallback := range c.ResultContract.AllowedFallbacks {
		h.text(string(fallback))
	}
	h.number(uint64(len(c.Parts)))
	for _, part := range c.Parts {
		h.text(string(part.Kind))
		h.text(part.Text)
		h.text(part.URI)
		h.text(part.MediaType)
	}
	h.text(string(c.Presence()))
	return h.sum()
}

// ManifestHash identifies every immutable field a manifest stores: its
// composition, payload hash, redaction policy version (NULL distinct from
// any value), search status, items and exclusions. The identity columns come
// from the intent and are not part of it.
func ManifestHash(m NewManifest) string {
	h := newCanonical("conduit.dispatch.manifest.v1")
	h.text(string(m.Composition))
	h.text(m.PayloadHash)
	h.flag(m.RedactionPolicyVersion != "")
	h.text(m.RedactionPolicyVersion)
	h.text(string(m.SearchStatus))
	h.number(uint64(len(m.Items)))
	for _, item := range m.Items {
		h.text(item.RecordID)
		h.text(item.VersionID)
		h.text(string(item.Mode))
		h.text(item.Reason)
		h.text(string(item.Trust))
	}
	h.number(uint64(len(m.Exclusions)))
	for _, exclusion := range m.Exclusions {
		h.text(exclusion.RecordID)
		h.text(exclusion.VersionID)
		h.flag(exclusion.IdentityWithheld)
		h.flag(exclusion.Mandatory)
		h.text(string(exclusion.Stage))
		h.text(string(exclusion.Kind))
		h.text(exclusion.Reason)
	}
	return h.sum()
}

// PayloadHash is the SHA-256 hex of text's exact bytes, the hash render
// reports as TextHash.
func PayloadHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
