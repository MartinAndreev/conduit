package rules

// PermissionProfileFacts describe the configured Codex sandbox mode and the
// workspace binding of one session request. Exactly one mode flag is set for a
// supported mode; none set means the configured mode is not supported.
type PermissionProfileFacts struct {
	ModeReadOnly       bool
	ModeWorkspaceWrite bool
	WorkspaceWritable  bool
}

type PermissionProfileKind string

const (
	// PermissionProfileWorkspaceWrite launches the Conduit profile with the
	// project roots writable.
	PermissionProfileWorkspaceWrite PermissionProfileKind = "workspace_write"
	// PermissionProfileWorkspaceRead launches the Conduit profile with the
	// project roots readable only.
	PermissionProfileWorkspaceRead PermissionProfileKind = "workspace_read"
	// PermissionProfileConflict refuses a writable mode for a read-only
	// workspace binding.
	PermissionProfileConflict PermissionProfileKind = "conflict"
	// PermissionProfileUnsupportedMode refuses a mode with no profile mapping.
	PermissionProfileUnsupportedMode PermissionProfileKind = "unsupported_mode"
)

type PermissionProfileDecision struct {
	Kind   PermissionProfileKind
	Reason string
}

// DecidePermissionProfile answers which project-root access the Conduit
// permission profile grants for the configured sandbox mode. The profile
// replaces the thread/start sandbox parameter, which overrides any profile.
func DecidePermissionProfile(f PermissionProfileFacts) PermissionProfileDecision {
	switch {
	case f.ModeReadOnly == f.ModeWorkspaceWrite:
		return PermissionProfileDecision{
			Kind:   PermissionProfileUnsupportedMode,
			Reason: "configured sandbox mode has no permission profile mapping",
		}
	case f.ModeReadOnly:
		return PermissionProfileDecision{
			Kind:   PermissionProfileWorkspaceRead,
			Reason: "read-only sandbox mode grants read access to the project roots",
		}
	case !f.WorkspaceWritable:
		return PermissionProfileDecision{
			Kind:   PermissionProfileConflict,
			Reason: "workspace-write sandbox conflicts with a read-only workspace binding",
		}
	default:
		return PermissionProfileDecision{
			Kind:   PermissionProfileWorkspaceWrite,
			Reason: "workspace-write sandbox mode grants write access to the project roots",
		}
	}
}
