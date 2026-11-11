package memory

// ConflictCounterpart is a record on the other side of a contradicts
// relation. Evaluated is true when the composition decided the counterpart's
// current version's eligibility; Eligible is that decision. Status is the
// counterpart's lifecycle as stored and matters only when it was not
// evaluated.
type ConflictCounterpart struct {
	RecordID  string
	Status    RecordStatus
	Evaluated bool
	Eligible  bool
}

// MaxContradictsPerItem is the most contradicts relations a reader loads for
// one record (initial value). Beyond it the reader reports the relations as
// truncated.
const MaxContradictsPerItem = 64

// ConflictFacts are one item's contradicts relations, in either direction.
// CounterpartsTruncated is true when the record has more relations than
// Counterparts lists.
type ConflictFacts struct {
	RecordID              string
	Counterparts          []ConflictCounterpart
	CounterpartsTruncated bool
}

type ConflictKind string

const (
	ConflictNone         ConflictKind = "no_conflict"
	ConflictLabelled     ConflictKind = "conflict"
	ConflictInvalidFacts ConflictKind = "invalid_facts"
)

type ConflictDecision struct {
	Kind   ConflictKind
	Reason string
}

// DecideConflictLabel answers whether an item enters labelled conflict. There
// is no silent resolution: contradicting memory that could also reach the
// agent, admitted or not, labels the item. A counterpart the composition did
// not evaluate is presumed eligible while its lifecycle is retrievable, since
// its absence from the candidates proves nothing about it. A counterpart that
// was evaluated and found ineligible (other version, not visible, not
// applicable, retired) is not live memory and does not label. Relations that
// were not all read label the item: an unread counterpart proves nothing.
func DecideConflictLabel(f ConflictFacts) ConflictDecision {
	if f.RecordID == "" {
		return ConflictDecision{Kind: ConflictInvalidFacts, Reason: "item has no record id"}
	}
	for _, c := range f.Counterparts {
		if c.RecordID == "" || c.RecordID == f.RecordID || !validStatus(c.Status) || (c.Eligible && !c.Evaluated) {
			return ConflictDecision{Kind: ConflictInvalidFacts, Reason: "counterpart needs another record id, a known status, and eligibility only when evaluated"}
		}
	}
	if f.CounterpartsTruncated {
		return ConflictDecision{Kind: ConflictLabelled, Reason: "contradicts more records than were read; an unread one is presumed live"}
	}
	for _, c := range f.Counterparts {
		if c.Evaluated && c.Eligible {
			return ConflictDecision{Kind: ConflictLabelled, Reason: "contradicts an eligible record"}
		}
		if !c.Evaluated && retrievableStatus(c.Status) {
			return ConflictDecision{Kind: ConflictLabelled, Reason: "contradicts a retrievable record that was not evaluated"}
		}
	}
	return ConflictDecision{Kind: ConflictNone, Reason: "no contradicting record could reach the agent"}
}

func retrievableStatus(status RecordStatus) bool {
	return status == StatusActive || status == StatusNeedsVerification
}
