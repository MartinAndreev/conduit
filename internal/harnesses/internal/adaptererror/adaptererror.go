package adaptererror

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func New(operation provider.Operation, code provider.ErrorCode, effect provider.RemoteEffect, cause error, detail string) error {
	return &provider.ProviderError{Code: code, Operation: operation, Retry: provider.RetryNever, RemoteEffect: effect, Cause: cause, Detail: detail}
}

func NewWithDetail(operation provider.Operation, code provider.ErrorCode, effect provider.RemoteEffect, detail string, cause error) error {
	return New(operation, code, effect, cause, detail)
}

func Delivery(operation provider.Operation, requestID provider.RequestID, sent bool, err error) error {
	if err == nil {
		return nil
	}
	code, effect, retry := provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, provider.RetryNever
	if sent {
		code, effect, retry = provider.ErrorDeliveryUnknown, provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile
	}
	if errors.Is(err, context.Canceled) {
		code = provider.ErrorCallerCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = provider.ErrorDeadline
	}
	return &provider.ProviderError{Code: code, Operation: operation, RequestID: provider.Some(requestID), Retry: retry, RemoteEffect: effect, Cause: err}
}
