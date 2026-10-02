package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestProviderErrorSupportsTypedAndCauseMatching(t *testing.T) {
	err := &provider.ProviderError{
		Code:         provider.ErrorDeliveryUnknown,
		Operation:    provider.OperationSteer,
		RequestID:    provider.Some(provider.RequestID("request-1")),
		Retry:        provider.RetryAfterReconcile,
		RemoteEffect: provider.RemoteEffectPossiblySent,
		Detail:       "receipt unavailable",
		Cause:        context.DeadlineExceeded,
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("wrapped context deadline is not discoverable with errors.Is")
	}
	if !errors.Is(err, &provider.ProviderError{Code: provider.ErrorDeliveryUnknown}) {
		t.Fatal("typed provider code is not discoverable with errors.Is")
	}
	if errors.Is(err, &provider.ProviderError{Code: provider.ErrorDeliveryRejected}) {
		t.Fatal("different provider code matched")
	}
}

func TestProviderErrorDoesNotRenderCauseText(t *testing.T) {
	err := &provider.ProviderError{
		Code:      provider.ErrorAuthFailed,
		Operation: provider.OperationOpenSession,
		Cause:     errors.New("secret native diagnostic"),
	}
	if strings.Contains(err.Error(), "secret native diagnostic") {
		t.Fatalf("Error() exposed wrapped diagnostic: %q", err.Error())
	}
}
