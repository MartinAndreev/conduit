package rules

import "github.com/MartinAndreev/conduit/internal/sessions/provider"

// DecideNativeContextPolicy answers what is proven about one native context
// source for this launch of an ACP agent. Evidence comes from the agent's
// descriptor; the protocol itself proves nothing about what an agent loads.
func DecideNativeContextPolicy(f provider.NativeContextFacts) provider.NativeContextDecision {
	return provider.DecideNativeContext(f)
}

// DecideProtectedPathsPolicy answers whether an ACP agent is proven unable to
// reach Conduit's protected directories for this launch.
func DecideProtectedPathsPolicy(f provider.ProtectedPathsFacts) provider.ProtectedPathsDecision {
	return provider.DecideProtectedPaths(f)
}
