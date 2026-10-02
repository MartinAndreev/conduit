package rules

import "testing"

func TestDecidePublishPrecedence(t *testing.T) {
	for _, tt := range []struct {
		consumer, closed bool
		want             PublishKind
	}{
		{false, false, PublishAllowed}, {true, false, PublishConsumerClosed}, {false, true, PublishStreamClosed}, {true, true, PublishConsumerClosed},
	} {
		got := DecidePublish(tt.consumer, tt.closed)
		if got.Kind != tt.want || got.Reason == "" {
			t.Fatalf("decision = %+v, want %s", got, tt.want)
		}
	}
}
