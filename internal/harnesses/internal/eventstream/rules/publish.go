package rules

// PublishKind describes eligibility to attempt a nonblocking publication.
type PublishKind string

const (
	PublishAllowed        PublishKind = "allowed"
	PublishConsumerClosed PublishKind = "consumer_closed"
	PublishStreamClosed   PublishKind = "stream_closed"
)

type PublishDecision struct {
	Kind   PublishKind
	Reason string
}

// DecidePublish does not inspect the buffer or interpret terminal errors.
// The caller keeps fact collection and the send attempt in one critical section.
func DecidePublish(consumerClosed, streamClosed bool) PublishDecision {
	if consumerClosed {
		return PublishDecision{
			Kind:   PublishConsumerClosed,
			Reason: "event consumer is closed",
		}
	}
	if streamClosed {
		return PublishDecision{
			Kind:   PublishStreamClosed,
			Reason: "event stream is closed",
		}
	}
	return PublishDecision{
		Kind:   PublishAllowed,
		Reason: "event stream is eligible for a publication attempt",
	}
}
