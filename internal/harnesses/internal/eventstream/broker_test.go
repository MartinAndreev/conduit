package eventstream

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func testEvent(t *testing.T, sequence uint64) provider.ProviderEvent {
	t.Helper()
	event, err := provider.NewProviderEvent(provider.EventEnvelope{AdapterSeq: sequence}, provider.SessionStateEvent{})
	if err != nil {
		t.Fatalf("NewProviderEvent: %v", err)
	}
	return event
}

func TestBrokerBuffersBeforeSubscription(t *testing.T) {
	broker := New(2)
	if err := broker.Publish(testEvent(t, 1)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	stream, err := broker.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	event, err := stream.Receive(context.Background())
	if err != nil || event.Envelope.AdapterSeq != 1 {
		t.Fatalf("Receive = (%+v, %v), want sequence 1", event, err)
	}
}

func TestBrokerOverflowIsExplicitAfterBufferedEvents(t *testing.T) {
	broker := New(1)
	if err := broker.Publish(testEvent(t, 1)); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if err := broker.Publish(testEvent(t, 2)); !errors.Is(err, ErrBufferOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
	stream, _ := broker.Subscribe()
	if _, err := stream.Receive(context.Background()); err != nil {
		t.Fatalf("buffered Receive: %v", err)
	}
	_, err := stream.Receive(context.Background())
	if !errors.Is(err, ErrBufferOverflow) {
		t.Fatalf("terminal Receive error = %v", err)
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorBackpressureExceeded || providerErr.Operation != provider.OperationReceiveEvent {
		t.Fatalf("overflow is not a typed backpressure error: %#v", err)
	}
}

func TestClosedConsumerIsVisibleToPublisherAndReceiver(t *testing.T) {
	broker := New(1)
	stream, _ := broker.Subscribe()
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := broker.Publish(testEvent(t, 1)); !errors.Is(err, ErrConsumerClosed) {
		t.Fatalf("Publish after close = %v", err)
	}
	if _, err := stream.Receive(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Receive after close = %v", err)
	}
}

func TestPublishClosedStreamPreservesTerminalAndConsumerPrecedence(t *testing.T) {
	terminal := errors.New("terminal transport failure")
	for _, err := range []error{nil, terminal, ErrBufferOverflow} {
		broker := New(1)
		broker.Finish(err)
		want := err
		if want == nil {
			want = io.ErrClosedPipe
		}
		if got := broker.Publish(testEvent(t, 1)); got != want {
			t.Fatalf("Publish = %v, want %v", got, want)
		}
		stream, _ := broker.Subscribe()
		_ = stream.Close()
		if got := broker.Publish(testEvent(t, 2)); got != ErrConsumerClosed {
			t.Fatalf("consumer precedence = %v", got)
		}
	}
}
