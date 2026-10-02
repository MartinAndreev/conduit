package eventstream

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream/rules"
	"io"
	"sync"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

var (
	ErrAlreadySubscribed = errors.New("provider event stream already subscribed")
	ErrBufferOverflow    = errors.New("provider event buffer overflow")
	ErrConsumerClosed    = errors.New("provider event consumer closed")
)

// Broker is a bounded, single-subscriber event buffer. It is created before
// native work starts, so startup events wait for the eventual subscriber.
type Broker struct {
	mu             sync.Mutex
	events         chan provider.ProviderEvent
	subscribed     bool
	consumerClosed bool
	closed         bool
	terminal       error
}

func New(capacity int) *Broker {
	if capacity < 1 {
		capacity = 1
	}
	return &Broker{events: make(chan provider.ProviderEvent, capacity)}
}

// Publish never blocks protocol readers. Overflow closes the stream with an
// explicit error rather than silently dropping an event.
func (b *Broker) Publish(event provider.ProviderEvent) error {
	return lockscope.WithMutexLock(&b.mu, func() error {
		switch rules.DecidePublish(b.consumerClosed, b.closed).Kind {
		case rules.PublishConsumerClosed:
			return ErrConsumerClosed
		case rules.PublishStreamClosed:
			if b.terminal != nil {
				return b.terminal
			}
			return io.ErrClosedPipe
		}
		select {
		case b.events <- event:
			return nil
		default:
			b.closed = true
			b.terminal = ErrBufferOverflow
			close(b.events)
			return ErrBufferOverflow
		}
	})
}

func (b *Broker) Finish(err error) {
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		if b.closed {
			return nil
		}
		b.closed = true
		b.terminal = err
		close(b.events)
		return nil
	})
}

func (b *Broker) Subscribe() (provider.EventStream, error) {
	var result provider.EventStream
	err := lockscope.WithMutexLock(&b.mu, func() error {
		if b.subscribed {
			return ErrAlreadySubscribed
		}
		b.subscribed = true
		result = &stream{broker: b, done: make(chan struct{})}
		return nil
	})
	return result, err
}

type stream struct {
	broker            *Broker
	terminalDelivered bool
	closeOnce         sync.Once
	done              chan struct{}
}

// Receive supports one reader. Consumers that need fan-out must use Conduit's
// journal/subscription layer rather than reading this adapter stream concurrently.
func (s *stream) Receive(ctx context.Context) (provider.ProviderEvent, error) {
	select {
	case <-ctx.Done():
		return provider.ProviderEvent{}, ctx.Err()
	case <-s.done:
		return provider.ProviderEvent{}, io.EOF
	case event, ok := <-s.broker.events:
		if ok {
			return event, nil
		}
		if s.terminalDelivered {
			return provider.ProviderEvent{}, io.EOF
		}
		s.terminalDelivered = true
		err := lockscope.WithMutexLock(&s.broker.mu, func() error {
			return s.broker.terminal
		})
		if err != nil {
			return provider.ProviderEvent{}, err
		}
		return provider.ProviderEvent{}, io.EOF
	}
}

func (s *stream) Close() error {
	s.closeOnce.Do(func() {
		_ = lockscope.WithMutexLock(&s.broker.mu, func() error {
			s.broker.consumerClosed = true
			return nil
		})
		close(s.done)
	})
	return nil
}
