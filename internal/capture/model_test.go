package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"testing/quick"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
)

func TestUDPExchangeJSONIsDiscriminated(t *testing.T) {
	exchange := Exchange{
		ID:        "udp-1",
		Transport: TransportUDP,
		Request:   Request{Method: http.MethodGet, Path: "/must-not-appear"},
		Datagram: &Datagram{
			SourceAddress: "127.0.0.1:5000",
			LocalAddress:  "127.0.0.1:9000",
			AcceptedBytes: 3,
			Payload:       []byte{0, 1, 2},
		},
	}

	encoded, err := json.Marshal(exchange)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if _, found := document["request"]; found {
		t.Fatalf("UDP exchange contains HTTP request: %s", encoded)
	}
	if _, found := document["datagram"]; !found {
		t.Fatalf("UDP exchange omits datagram: %s", encoded)
	}
}

func TestTransition(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	tests := []struct {
		name      string
		from      State
		to        State
		wantError bool
	}{
		{name: "receiving to forwarding", from: StateReceiving, to: StateForwarding},
		{name: "receiving to completed", from: StateReceiving, to: StateCompleted},
		{name: "receiving to discarded", from: StateReceiving, to: StateDiscarded},
		{name: "forwarding to completed", from: StateForwarding, to: StateCompleted},
		{name: "receiving to failed", from: StateReceiving, to: StateFailed},
		{name: "receiving to cancelled", from: StateReceiving, to: StateCancelled},
		{name: "terminal cannot change", from: StateCompleted, to: StateFailed, wantError: true},
		{name: "capture cannot move backwards", from: StateForwarding, to: StateReceiving, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exchange := Exchange{State: test.from, StartedAt: now.Add(-time.Second), Revision: 1}
			err := exchange.Transition(test.to, now)
			if (err != nil) != test.wantError {
				t.Fatalf("Transition() error = %v, wantError %v", err, test.wantError)
			}
			if !test.wantError && exchange.Revision != 2 {
				t.Fatalf("revision = %d, want 2", exchange.Revision)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{state: StateReceiving},
		{state: StateForwarding},
		{state: StateCompleted, want: true},
		{state: StateFailed, want: true},
		{state: StateCancelled, want: true},
		{state: StateDiscarded, want: true},
	}
	for _, test := range tests {
		if got := IsTerminal(test.state); got != test.want {
			t.Fatalf("IsTerminal(%q)=%v want=%v", test.state, got, test.want)
		}
	}
}

func TestTransitionUsesMonotonicStart(t *testing.T) {
	started := time.Now()
	exchange := Exchange{State: StateReceiving, StartedAt: started.Add(24 * time.Hour).UTC(), startedMonotonic: started, Revision: 1}
	if err := exchange.Transition(StateCompleted, started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if exchange.Duration != time.Second {
		t.Fatalf("duration = %s, want 1s", exchange.Duration)
	}
}

func TestHeaderBytesEstimateProperty(t *testing.T) {
	property := func(name, first, second, host string) bool {
		headers := http.Header{name: []string{first, second}}
		got := HeaderBytesEstimate(headers, host)
		return got == HeaderBytesEstimate(headers, host) && got >= 2
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestHeaderBytesEstimateIncludesRepeatedValuesAndHost(t *testing.T) {
	headers := http.Header{"X-Test": {"one", "two"}, "Host": {"ignored"}}
	want := int64(len("X-Test: one\r\nX-Test: two\r\nHost: example.test\r\n\r\n"))
	if got := HeaderBytesEstimate(headers, "example.test"); got != want {
		t.Fatalf("HeaderBytesEstimate() = %d, want %d", got, want)
	}
}

func TestReadBody(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		limit        int64
		want         []byte
		wantObserved int64
		wantError    error
	}{
		{name: "empty", limit: 2},
		{name: "exact", body: []byte{0, 1}, limit: 2, want: []byte{0, 1}, wantObserved: 2},
		{name: "too large", body: []byte{0, 1, 2}, limit: 2, want: []byte{0, 1}, wantObserved: 3, wantError: ErrBodyTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, observed, err := ReadBody(bytes.NewReader(test.body), test.limit)
			if !bytes.Equal(got, test.want) || observed != test.wantObserved || !errors.Is(err, test.wantError) {
				t.Fatalf("ReadBody() = (%v, %d, %v), want (%v, %d, %v)", got, observed, err, test.want, test.wantObserved, test.wantError)
			}
		})
	}
}

func TestSnapshotClonesMutableConfiguration(t *testing.T) {
	cfg := config.Defaults()
	cfg.Capture.Headers = map[string][]string{"X-Test": {"original"}}
	snapshot := NewConfigSnapshot(cfg, 7)
	cfg.Capture.Headers["X-Test"][0] = "changed"
	if snapshot.Capture.Headers["X-Test"][0] != "original" || snapshot.Revision != 7 {
		t.Fatalf("snapshot changed: %+v", snapshot)
	}
}

func TestExchangeCloneDoesNotShareMutableData(t *testing.T) {
	value := int64(1)
	stamp := time.Now()
	exchange := Exchange{
		Configuration: ConfigSnapshot{Capture: config.CaptureResponse{Headers: map[string][]string{"X-Config": {"one"}}}},
		Request:       Request{Headers: http.Header{"X-Request": {"one"}}, Body: []byte{1}, Preview: []byte{1}},
		Datagram:      &Datagram{Payload: []byte{3}, Preview: []byte{3}},
		Response:      &Response{Headers: http.Header{"X-Response": {"one"}}, Body: []byte{2}, Preview: []byte{2}, DecodedPreviewBytes: &value},
		ProxyTiming:   &ProxyTiming{UpstreamStartedAt: &stamp, UpstreamTotalDurationUS: &value},
		Error:         &ExchangeError{Category: "test"},
		Persistence:   &PersistenceStatus{State: "failed", Error: "disk"},
	}
	clone := exchange.Clone()
	clone.Configuration.Capture.Headers["X-Config"][0] = "changed"
	clone.Request.Headers["X-Request"][0] = "changed"
	clone.Request.Body[0] = 9
	clone.Datagram.Payload[0] = 9
	clone.Response.Body[0] = 9
	clone.Response.Preview[0] = 9
	*clone.Response.DecodedPreviewBytes = 9
	*clone.ProxyTiming.UpstreamTotalDurationUS = 9
	clone.Error.Category = "changed"
	clone.Persistence.State = "persisted"
	if exchange.Configuration.Capture.Headers["X-Config"][0] != "one" || exchange.Request.Headers["X-Request"][0] != "one" || exchange.Request.Body[0] != 1 || exchange.Datagram.Payload[0] != 3 || exchange.Response.Body[0] != 2 || exchange.Response.Preview[0] != 2 || *exchange.Response.DecodedPreviewBytes != 1 || *exchange.ProxyTiming.UpstreamTotalDurationUS != 1 || exchange.Error.Category != "test" || exchange.Persistence.State != "failed" {
		t.Fatal("clone shares mutable exchange data")
	}
}

func FuzzReadBody(f *testing.F) {
	f.Add([]byte("request"), uint16(3))
	f.Add([]byte{0, 255, 1}, uint16(3))
	f.Fuzz(func(t *testing.T, data []byte, rawLimit uint16) {
		limit := int64(rawLimit) + 1
		body, observed, err := ReadBody(bytes.NewReader(data), limit)
		if int64(len(data)) <= limit {
			if err != nil || !bytes.Equal(body, data) || observed != int64(len(data)) {
				t.Fatalf("accepted body mismatch")
			}
			return
		}
		if !errors.Is(err, ErrBodyTooLarge) || int64(len(body)) != limit || observed != limit+1 {
			t.Fatalf("oversized result len=%d observed=%d error=%v", len(body), observed, err)
		}
	})
}

func FuzzUDPDatagramCloneAndJSON(f *testing.F) {
	f.Add([]byte("udp"))
	f.Add([]byte{0, 255, 10})
	f.Fuzz(func(t *testing.T, payload []byte) {
		exchange := Exchange{ID: "udp", Transport: TransportUDP, Mode: "capture", State: StateCompleted, Datagram: &Datagram{SourceAddress: "127.0.0.1:1", LocalAddress: "127.0.0.1:2", AcceptedBytes: int64(len(payload)), Payload: append([]byte(nil), payload...), Preview: append([]byte(nil), payload...), Delivery: DatagramDelivery{Result: "not_sent"}}}
		clone := exchange.Clone()
		if len(payload) > 0 {
			payload[0] ^= 0xff
		}
		if _, err := json.Marshal(clone); err != nil {
			t.Fatal(err)
		}
	})
}
