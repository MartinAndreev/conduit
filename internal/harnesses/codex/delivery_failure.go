package codex

import (
	"errors"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// deliveryFailure separates a proven native rejection from transport uncertainty.
// State release and reconciliation remain with each operation's procedure.
type deliveryFailure struct {
	NativeRejected bool
	Err            error
}

func classifyDeliveryFailure(operation provider.Operation, requestID provider.RequestID, sent bool, cause error) deliveryFailure {
	var nativeErr *rpcError
	if errors.As(cause, &nativeErr) {
		return deliveryFailure{NativeRejected: true, Err: &provider.ProviderError{
			Code: provider.ErrorDeliveryRejected, Operation: operation, RequestID: provider.Some(requestID),
			Retry: provider.RetryNever, RemoteEffect: provider.RemoteEffectNotSent, Cause: cause,
		}}
	}
	return deliveryFailure{Err: adaptererror.Delivery(operation, requestID, sent, cause)}
}
