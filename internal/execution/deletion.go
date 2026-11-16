package execution

import "github.com/MartinAndreev/conduit/internal/memory"

// IntentDispatchState maps an intent state to the dispatch state the payload
// removal rule (memory.DecidePayloadRemoval) uses when a record the manifest
// carried is deleted. Every state maps: recorded is intent_recorded;
// dispatching and unknown may have been sent; accepted and diverged were
// delivered; rejected and invalidated were never sent. ok is false only for
// an unknown state.
func IntentDispatchState(state IntentState) (memory.DispatchState, bool) {
	switch state {
	case StateRecorded:
		return memory.DispatchIntentRecorded, true
	case StateDispatching, StateUnknown:
		return memory.DispatchDeliveryUnknown, true
	case StateAccepted, StateDiverged:
		return memory.DispatchDelivered, true
	case StateRejected, StateInvalidated:
		return memory.DispatchNotDispatched, true
	default:
		return "", false
	}
}
