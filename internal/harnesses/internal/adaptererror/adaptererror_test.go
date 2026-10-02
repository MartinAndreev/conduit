package adaptererror

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"testing"
)

func TestDeliveryMapping(t *testing.T) {
	ordinary := errors.New("transport")
	tests := []struct {
		name   string
		err    error
		sent   bool
		code   provider.ErrorCode
		effect provider.RemoteEffect
		retry  provider.RetryClass
	}{
		{"ordinary-unsent", ordinary, false, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, provider.RetryNever}, {"ordinary-sent", ordinary, true, provider.ErrorDeliveryUnknown, provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile},
		{"cancel-unsent", context.Canceled, false, provider.ErrorCallerCancelled, provider.RemoteEffectNotSent, provider.RetryNever}, {"cancel-sent", context.Canceled, true, provider.ErrorCallerCancelled, provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile},
		{"deadline-unsent", context.DeadlineExceeded, false, provider.ErrorDeadline, provider.RemoteEffectNotSent, provider.RetryNever}, {"deadline-sent", context.DeadlineExceeded, true, provider.ErrorDeadline, provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Delivery(provider.OperationSteer, "request", test.sent, test.err)
			var got *provider.ProviderError
			if !errors.As(err, &got) || got.Code != test.code || got.RemoteEffect != test.effect || got.Retry != test.retry || got.RequestID.Value != "request" || got.Operation != provider.OperationSteer || !errors.Is(err, test.err) {
				t.Fatalf("unexpected mapping: %#v", got)
			}
		})
	}
}
