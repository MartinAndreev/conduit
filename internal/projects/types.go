// Package projects owns catalog identities and pure registration policies.
package projects

import "slices"

type ID string
type ApplicationID string

const (
	TUI ApplicationID = "tui"
	GUI ApplicationID = "gui"
)

func Applications() []ApplicationID           { return []ApplicationID{TUI, GUI} }
func ValidApplication(app ApplicationID) bool { return slices.Contains(Applications(), app) }

type Project struct {
	ID            ID
	Name          string
	CanonicalPath string
}
type AvailabilityKind string

const (
	Available              AvailabilityKind = "available"
	Missing                AvailabilityKind = "missing"
	Unreadable             AvailabilityKind = "unreadable"
	NotDirectory           AvailabilityKind = "not_directory"
	InvalidPath            AvailabilityKind = "invalid_path"
	CanonicalizationFailed AvailabilityKind = "canonicalization_failed"
)

type Directory struct {
	CanonicalPath string
	Name          string
	Kind          AvailabilityKind
	Cause         error
}
type RegistrationKind string

const (
	RegistrationAllowed  RegistrationKind = "allowed"
	RegistrationExisting RegistrationKind = "existing"
	RegistrationRejected RegistrationKind = "rejected"
)

type RegistrationFacts struct {
	Directory Directory
	Existing  *Project
}
type RegistrationDecision struct {
	Kind   RegistrationKind
	Reason string
}

func DecideRegistration(f RegistrationFacts) RegistrationDecision {
	if f.Directory.Kind != Available || f.Directory.CanonicalPath == "" || f.Directory.Name == "" {
		return RegistrationDecision{RegistrationRejected, "A registration requires an available canonical directory."}
	}
	if f.Existing != nil {
		return RegistrationDecision{RegistrationExisting, "The directory is already registered."}
	}
	return RegistrationDecision{RegistrationAllowed, "The directory can be registered."}
}

type OpeningKind string

const (
	OpeningAllowed     OpeningKind = "allowed"
	OpeningUnavailable OpeningKind = "unavailable"
)

type OpeningDecision struct {
	Kind   OpeningKind
	Reason string
}

func DecideOpening(directory Directory) OpeningDecision {
	if directory.Kind != Available {
		return OpeningDecision{OpeningUnavailable, "The registered directory is unavailable."}
	}
	return OpeningDecision{OpeningAllowed, "The registered directory is available."}
}
