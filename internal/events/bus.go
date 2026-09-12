package events

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

const SchemaVersion = 1

type Type string

const (
	ExchangeStarted   Type = "exchange.started"
	RequestCompleted  Type = "request.completed"
	ResponseCompleted Type = "response.completed"
	ExchangeFailed    Type = "exchange.failed"
	ExchangeEvicted   Type = "exchange.evicted"
	ExchangeDeleted   Type = "exchange.deleted"
	HistoryCleared    Type = "history.cleared"
	ConfigChanged     Type = "config.changed"
)

type Envelope struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"event_id"`
	Timestamp     time.Time `json:"timestamp"`
	Type          Type      `json:"type"`
	ExchangeID    string    `json:"exchange_id,omitempty"`
	Revision      uint64    `json:"revision,omitempty"`
	Data          any       `json:"data,omitempty"`
}

type Limits struct {
	ReplayCount     int
	ReplayBytes     int
	SubscriberCount int
	SubscriberBytes int
}

type queuedEnvelope struct {
	envelope Envelope
	size     int
}

type subscriber struct {
	queue        chan queuedEnvelope
	events       chan Envelope
	done         chan struct{}
	pendingCount int
	pendingBytes int
}

type Bus struct {
	mutex       sync.Mutex
	limits      Limits
	now         func() time.Time
	nextID      uint64
	replay      []queuedEnvelope
	replayBytes int
	subscribers map[*subscriber]struct{}
}

type Subscription struct {
	Events <-chan Envelope
	Replay []Envelope
	Reset  bool
	close  func()
	once   sync.Once
}

type SnapshotSubscription struct {
	Subscription *Subscription
	Snapshot     any
	Cursor       string
}

func NewBus(limits Limits, now func() time.Time) *Bus {
	limits = normalizedLimits(limits)
	if now == nil {
		now = time.Now
	}
	return &Bus{limits: limits, now: now, subscribers: make(map[*subscriber]struct{})}
}

func (bus *Bus) Publish(envelope Envelope) Envelope {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.nextID++
	envelope.SchemaVersion = SchemaVersion
	envelope.ID = strconv.FormatUint(bus.nextID, 10)
	envelope.Timestamp = bus.now().UTC()
	size := envelopeSize(envelope)
	queued := queuedEnvelope{envelope: envelope, size: size}
	bus.replay = append(bus.replay, queued)
	bus.replayBytes += size
	bus.trimReplay()
	for target := range bus.subscribers {
		if size > bus.limits.SubscriberBytes || target.pendingCount >= bus.limits.SubscriberCount || target.pendingBytes+size > bus.limits.SubscriberBytes {
			bus.disconnect(target)
			continue
		}
		target.pendingCount++
		target.pendingBytes += size
		select {
		case target.queue <- queued:
		default:
			bus.disconnect(target)
		}
	}
	return envelope
}

func (bus *Bus) Subscribe(afterID string) *Subscription {
	bus.mutex.Lock()
	subscription := bus.subscribeLocked(afterID)
	bus.mutex.Unlock()
	return subscription
}

func (bus *Bus) SubscribeWithSnapshot(snapshot func() any) SnapshotSubscription {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	value := snapshot()
	cursor := ""
	if len(bus.replay) > 0 {
		cursor = bus.replay[len(bus.replay)-1].envelope.ID
	}
	return SnapshotSubscription{Subscription: bus.subscribeLocked(cursor), Snapshot: value, Cursor: cursor}
}

func (bus *Bus) subscribeLocked(afterID string) *Subscription {
	replay, reset := bus.replayAfter(afterID)
	target := &subscriber{queue: make(chan queuedEnvelope, bus.limits.SubscriberCount), events: make(chan Envelope), done: make(chan struct{})}
	bus.subscribers[target] = struct{}{}
	go bus.forward(target)
	return &Subscription{Events: target.events, Replay: replay, Reset: reset, close: func() {
		bus.mutex.Lock()
		defer bus.mutex.Unlock()
		bus.disconnect(target)
	}}
}

func (subscription *Subscription) Close() {
	subscription.once.Do(subscription.close)
}

func (bus *Bus) forward(target *subscriber) {
	defer close(target.events)
	for {
		select {
		case <-target.done:
			return
		case queued := <-target.queue:
			select {
			case <-target.done:
				return
			case target.events <- queued.envelope:
				bus.mutex.Lock()
				target.pendingCount--
				target.pendingBytes -= queued.size
				bus.mutex.Unlock()
			}
		}
	}
}

func (bus *Bus) disconnect(target *subscriber) {
	if _, ok := bus.subscribers[target]; !ok {
		return
	}
	delete(bus.subscribers, target)
	close(target.done)
}

func (bus *Bus) trimReplay() {
	for len(bus.replay) > 0 && (len(bus.replay) > bus.limits.ReplayCount || bus.replayBytes > bus.limits.ReplayBytes) {
		bus.replayBytes -= bus.replay[0].size
		bus.replay = bus.replay[1:]
	}
}

func (bus *Bus) replayAfter(afterID string) ([]Envelope, bool) {
	if afterID == "" {
		return envelopes(bus.replay), false
	}
	for index, queued := range bus.replay {
		if queued.envelope.ID == afterID {
			return envelopes(bus.replay[index+1:]), false
		}
	}
	return nil, true
}

func envelopes(queued []queuedEnvelope) []Envelope {
	result := make([]Envelope, len(queued))
	for index := range queued {
		result[index] = queued[index].envelope
	}
	return result
}

func envelopeSize(envelope Envelope) int {
	data, err := json.Marshal(envelope)
	if err != nil {
		return 1
	}
	return len(data)
}

func normalizedLimits(limits Limits) Limits {
	if limits.ReplayCount < 1 {
		limits.ReplayCount = 1
	}
	if limits.ReplayBytes < 1 {
		limits.ReplayBytes = 1
	}
	if limits.SubscriberCount < 1 {
		limits.SubscriberCount = 1
	}
	if limits.SubscriberBytes < 1 {
		limits.SubscriberBytes = 1
	}
	return limits
}
