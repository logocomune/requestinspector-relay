package events

import (
	"testing"
	"time"
)

func TestBusAssignsVersionedEnvelopeAndReplays(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	bus := NewBus(Limits{ReplayCount: 2, ReplayBytes: 1024, SubscriberCount: 2, SubscriberBytes: 1024}, func() time.Time { return now })
	first := bus.Publish(Envelope{Type: ExchangeStarted, ExchangeID: "one", Revision: 1})
	second := bus.Publish(Envelope{Type: RequestCompleted, ExchangeID: "one", Revision: 2})
	if first.SchemaVersion != SchemaVersion || first.ID == "" || !first.Timestamp.Equal(now) || second.ID == first.ID {
		t.Fatalf("invalid envelopes: %+v %+v", first, second)
	}
	subscription := bus.Subscribe(first.ID)
	defer subscription.Close()
	if subscription.Reset || len(subscription.Replay) != 1 || subscription.Replay[0].ID != second.ID {
		t.Fatalf("subscription = %+v", subscription)
	}
}

func TestBusRequestsResetForReplayGap(t *testing.T) {
	bus := NewBus(Limits{ReplayCount: 1, ReplayBytes: 1024, SubscriberCount: 1, SubscriberBytes: 1024}, time.Now)
	first := bus.Publish(Envelope{Type: ExchangeStarted})
	bus.Publish(Envelope{Type: RequestCompleted})
	subscription := bus.Subscribe(first.ID)
	defer subscription.Close()
	if !subscription.Reset || len(subscription.Replay) != 0 {
		t.Fatalf("expected replay reset: %+v", subscription)
	}
}

func TestBusDisconnectsSlowSubscriber(t *testing.T) {
	bus := NewBus(Limits{ReplayCount: 4, ReplayBytes: 1024, SubscriberCount: 1, SubscriberBytes: 1}, time.Now)
	subscription := bus.Subscribe("")
	bus.Publish(Envelope{Type: ExchangeStarted})
	bus.Publish(Envelope{Type: RequestCompleted})
	for range subscription.Events {
	}
}

func TestBusDeliversLiveEvent(t *testing.T) {
	bus := NewBus(Limits{ReplayCount: 2, ReplayBytes: 1024, SubscriberCount: 2, SubscriberBytes: 1024}, time.Now)
	subscription := bus.Subscribe("")
	defer subscription.Close()
	published := bus.Publish(Envelope{Type: ExchangeStarted})
	select {
	case received := <-subscription.Events:
		if received.ID != published.ID {
			t.Fatalf("received ID = %q, want %q", received.ID, published.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("live event not delivered")
	}
}

func TestBusBoundsReplayByBytes(t *testing.T) {
	bus := NewBus(Limits{ReplayCount: 10, ReplayBytes: 1, SubscriberCount: 1, SubscriberBytes: 1024}, time.Now)
	published := bus.Publish(Envelope{Type: ExchangeStarted})
	subscription := bus.Subscribe("")
	defer subscription.Close()
	if len(subscription.Replay) != 0 {
		t.Fatalf("oversized replay retained: %+v", subscription.Replay)
	}
	gap := bus.Subscribe(published.ID)
	defer gap.Close()
	if !gap.Reset {
		t.Fatal("evicted replay ID did not request reset")
	}
}

func TestBusNormalizesLimitsAndNilClock(t *testing.T) {
	bus := NewBus(Limits{}, nil)
	envelope := bus.Publish(Envelope{Type: ExchangeStarted})
	if envelope.Timestamp.IsZero() {
		t.Fatal("nil clock produced zero timestamp")
	}
}

func TestBusSnapshotHandoffHasNoEventGap(t *testing.T) {
	bus := NewBus(Limits{ReplayCount: 4, ReplayBytes: 1024, SubscriberCount: 4, SubscriberBytes: 1024}, time.Now)
	first := bus.Publish(Envelope{Type: ExchangeStarted, ExchangeID: "one"})
	result := bus.SubscribeWithSnapshot(func() any { return []string{"one"} })
	defer result.Subscription.Close()
	if result.Cursor != first.ID || result.Subscription.Reset || len(result.Subscription.Replay) != 0 {
		t.Fatalf("snapshot subscription = %+v", result)
	}
	second := bus.Publish(Envelope{Type: RequestCompleted, ExchangeID: "one"})
	select {
	case received := <-result.Subscription.Events:
		if received.ID != second.ID {
			t.Fatalf("received event %q, want %q", received.ID, second.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("event after snapshot was lost")
	}
}
