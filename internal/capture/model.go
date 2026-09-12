package capture

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
)

type State string

type Transport string

const (
	TransportHTTP Transport = "http"
	TransportUDP  Transport = "udp"
)

const (
	StateReceiving  State = "receiving"
	StateForwarding State = "forwarding"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
	StateCancelled  State = "cancelled"
	StateDiscarded  State = "discarded"
)

const (
	ErrorDatagramOversized = "udp_datagram_oversized"
	ErrorDatagramOverload  = "udp_datagram_overload"
	ErrorDatagramDelivery  = "udp_datagram_delivery_failed"
)

type ConfigSnapshot struct {
	Revision uint64
	Mode     string
	Upstream config.Upstream
	Capture  config.CaptureResponse
	Limits   config.Limits
	Preview  config.Preview
	Storage  config.Storage
}

type Request struct {
	Method              string      `json:"method"`
	Scheme              string      `json:"scheme"`
	Protocol            string      `json:"protocol"`
	Host                string      `json:"host"`
	RequestTarget       string      `json:"request_target"`
	Path                string      `json:"path"`
	RawQuery            string      `json:"raw_query"`
	RemoteAddress       string      `json:"remote_address"`
	Headers             http.Header `json:"headers"`
	ContentType         string      `json:"content_type"`
	Body                []byte      `json:"-"`
	ObservedBodyBytes   int64       `json:"observed_body_bytes"`
	BodyComplete        bool        `json:"body_complete"`
	HeaderBytesEstimate int64       `json:"header_bytes_estimate"`
	Preview             []byte      `json:"preview,omitempty"`
	PreviewTruncated    bool        `json:"preview_truncated"`
	DecodedPreviewBytes *int64      `json:"decoded_preview_bytes,omitempty"`
	PreviewError        string      `json:"preview_error,omitempty"`
}

type Response struct {
	Status              int         `json:"status"`
	Headers             http.Header `json:"headers"`
	Body                []byte      `json:"-"`
	ObservedBodyBytes   int64       `json:"observed_body_bytes"`
	BodyComplete        bool        `json:"body_complete"`
	HeaderBytesEstimate int64       `json:"header_bytes_estimate"`
	Origin              string      `json:"origin"`
	Preview             []byte      `json:"preview,omitempty"`
	PreviewTruncated    bool        `json:"preview_truncated"`
	DecodedPreviewBytes *int64      `json:"decoded_preview_bytes,omitempty"`
	PreviewError        string      `json:"preview_error,omitempty"`
}

type ProxyTiming struct {
	UpstreamStartedAt          *time.Time `json:"upstream_started_at,omitempty"`
	RequestWrittenAt           *time.Time `json:"request_written_at,omitempty"`
	FirstResponseByteAt        *time.Time `json:"first_response_byte_at,omitempty"`
	ResponseCapturedAt         *time.Time `json:"response_captured_at,omitempty"`
	DownstreamCompletedAt      *time.Time `json:"downstream_completed_at,omitempty"`
	UpstreamDispatchDurationUS *int64     `json:"upstream_dispatch_duration_us,omitempty"`
	UpstreamWaitDurationUS     *int64     `json:"upstream_wait_duration_us,omitempty"`
	UpstreamReceiveDurationUS  *int64     `json:"upstream_receive_duration_us,omitempty"`
	UpstreamTotalDurationUS    *int64     `json:"upstream_total_duration_us,omitempty"`
	DownstreamWriteDurationUS  *int64     `json:"downstream_write_duration_us,omitempty"`
	ProxyTotalDurationUS       *int64     `json:"proxy_total_duration_us,omitempty"`
}

type ExchangeError struct {
	Category string `json:"category"`
	Message  string `json:"message"`
}

type DownstreamDelivery struct {
	Status    int    `json:"status"`
	BodyBytes int64  `json:"body_bytes"`
	Error     string `json:"error,omitempty"`
}

// Datagram holds UDP-specific metadata without inventing HTTP request fields.
type Datagram struct {
	SourceAddress    string           `json:"source_address"`
	LocalAddress     string           `json:"local_address"`
	AcceptedBytes    int64            `json:"accepted_bytes"`
	Payload          []byte           `json:"-"`
	PayloadComplete  bool             `json:"payload_complete"`
	Preview          []byte           `json:"preview,omitempty"`
	PreviewTruncated bool             `json:"preview_truncated"`
	DiscardReason    string           `json:"discard_reason,omitempty"`
	Delivery         DatagramDelivery `json:"delivery"`
}

// DatagramDelivery describes UDP delivery without HTTP response semantics.
type DatagramDelivery struct {
	Result    string `json:"result"`
	SentBytes int64  `json:"sent_bytes"`
	Message   string `json:"message,omitempty"`
	Truncated bool   `json:"truncated"`
}

type PersistenceStatus struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

type Exchange struct {
	ID                                  string             `json:"id"`
	Transport                           Transport          `json:"transport"`
	Mode                                string             `json:"mode"`
	ConfigRevision                      uint64             `json:"config_revision"`
	Configuration                       ConfigSnapshot     `json:"-"`
	Revision                            uint64             `json:"revision"`
	State                               State              `json:"state"`
	StartedAt                           time.Time          `json:"started_at"`
	CompletedAt                         time.Time          `json:"completed_at,omitempty"`
	Duration                            time.Duration      `json:"duration_ns"`
	Request                             Request            `json:"request"`
	Datagram                            *Datagram          `json:"datagram,omitempty"`
	Response                            *Response          `json:"response,omitempty"`
	EffectiveUpstream                   string             `json:"effective_upstream,omitempty"`
	UpstreamResponseHeaderBytesEstimate int64              `json:"upstream_response_header_bytes_estimate,omitempty"`
	UpstreamObservedResponseBodyBytes   int64              `json:"upstream_observed_response_body_bytes,omitempty"`
	ProxyTiming                         *ProxyTiming       `json:"proxy_timing,omitempty"`
	Error                               *ExchangeError     `json:"error,omitempty"`
	DownstreamDelivery                  DownstreamDelivery `json:"downstream_delivery"`
	Persistence                         *PersistenceStatus `json:"persistence,omitempty"`
	startedMonotonic                    time.Time
}

func NewConfigSnapshot(cfg config.Config, revision uint64) ConfigSnapshot {
	return ConfigSnapshot{
		Revision: revision,
		Mode:     cfg.Mode,
		Upstream: cfg.Upstream,
		Capture: config.CaptureResponse{
			Status:  cfg.Capture.Status,
			Headers: cloneHeaders(cfg.Capture.Headers),
			Body:    cfg.Capture.Body,
		},
		Limits:  cfg.Limits,
		Preview: cfg.Preview,
		Storage: cfg.Storage,
	}
}

func (snapshot ConfigSnapshot) Clone() ConfigSnapshot {
	clone := snapshot
	clone.Capture.Headers = cloneHeaders(snapshot.Capture.Headers)
	return clone
}

func (request Request) Clone() Request {
	clone := request
	clone.Headers = request.Headers.Clone()
	clone.Body = append([]byte(nil), request.Body...)
	clone.Preview = append([]byte(nil), request.Preview...)
	clone.DecodedPreviewBytes = cloneInt64(request.DecodedPreviewBytes)
	return clone
}

func (exchange *Exchange) Transition(next State, at time.Time) error {
	if !validTransition(exchange.State, next) {
		return fmt.Errorf("invalid exchange transition %q to %q", exchange.State, next)
	}
	exchange.State = next
	exchange.Revision++
	if IsTerminal(next) {
		exchange.CompletedAt = at.UTC()
		started := exchange.StartedAt
		if !exchange.startedMonotonic.IsZero() {
			started = exchange.startedMonotonic
		}
		exchange.Duration = at.Sub(started)
		if exchange.Duration < 0 {
			exchange.Duration = 0
		}
	}
	return nil
}

func (exchange Exchange) Clone() Exchange {
	clone := exchange
	clone.Configuration = exchange.Configuration.Clone()
	clone.Request = exchange.Request.Clone()
	clone.Datagram = exchange.Datagram.Clone()
	if exchange.Response != nil {
		response := *exchange.Response
		response.Headers = cloneHeaders(exchange.Response.Headers)
		response.Body = append([]byte(nil), exchange.Response.Body...)
		response.Preview = append([]byte(nil), exchange.Response.Preview...)
		response.DecodedPreviewBytes = cloneInt64(exchange.Response.DecodedPreviewBytes)
		clone.Response = &response
	}
	clone.ProxyTiming = cloneProxyTiming(exchange.ProxyTiming)
	if exchange.Error != nil {
		exchangeError := *exchange.Error
		clone.Error = &exchangeError
	}
	if exchange.Persistence != nil {
		persistence := *exchange.Persistence
		clone.Persistence = &persistence
	}
	return clone
}

func (datagram *Datagram) Clone() *Datagram {
	if datagram == nil {
		return nil
	}
	clone := *datagram
	clone.Payload = append([]byte(nil), datagram.Payload...)
	clone.Preview = append([]byte(nil), datagram.Preview...)
	return &clone
}

func (exchange Exchange) MarshalJSON() ([]byte, error) {
	type raw Exchange
	request := &exchange.Request
	downstreamDelivery := &exchange.DownstreamDelivery
	if exchange.Transport == TransportUDP {
		request = nil
		downstreamDelivery = nil
	}
	return json.Marshal(struct {
		raw
		Request            *Request            `json:"request,omitempty"`
		DownstreamDelivery *DownstreamDelivery `json:"downstream_delivery,omitempty"`
	}{
		raw:                raw(exchange),
		Request:            request,
		DownstreamDelivery: downstreamDelivery,
	})
}

func cloneProxyTiming(timing *ProxyTiming) *ProxyTiming {
	if timing == nil {
		return nil
	}
	clone := *timing
	clone.UpstreamStartedAt = cloneTime(timing.UpstreamStartedAt)
	clone.RequestWrittenAt = cloneTime(timing.RequestWrittenAt)
	clone.FirstResponseByteAt = cloneTime(timing.FirstResponseByteAt)
	clone.ResponseCapturedAt = cloneTime(timing.ResponseCapturedAt)
	clone.DownstreamCompletedAt = cloneTime(timing.DownstreamCompletedAt)
	clone.UpstreamDispatchDurationUS = cloneInt64(timing.UpstreamDispatchDurationUS)
	clone.UpstreamWaitDurationUS = cloneInt64(timing.UpstreamWaitDurationUS)
	clone.UpstreamReceiveDurationUS = cloneInt64(timing.UpstreamReceiveDurationUS)
	clone.UpstreamTotalDurationUS = cloneInt64(timing.UpstreamTotalDurationUS)
	clone.DownstreamWriteDurationUS = cloneInt64(timing.DownstreamWriteDurationUS)
	clone.ProxyTotalDurationUS = cloneInt64(timing.ProxyTotalDurationUS)
	return &clone
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func HeaderBytesEstimate(headers http.Header, host string) int64 {
	size := int64(2)
	for name, values := range headers {
		if http.CanonicalHeaderKey(name) == "Host" {
			continue
		}
		for _, value := range values {
			size += int64(len(name) + 2 + len(value) + 2)
		}
	}
	if host != "" {
		size += int64(len("Host") + 2 + len(host) + 2)
	}
	return size
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate exchange ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func validTransition(current, next State) bool {
	switch current {
	case StateReceiving:
		return next == StateForwarding || next == StateCompleted || next == StateFailed || next == StateCancelled || next == StateDiscarded
	case StateForwarding:
		return next == StateCompleted || next == StateFailed || next == StateCancelled
	default:
		return false
	}
}

func IsTerminal(state State) bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled || state == StateDiscarded
}

func cloneHeaders(headers map[string][]string) http.Header {
	if headers == nil {
		return nil
	}
	clone := make(http.Header, len(headers))
	for name, values := range headers {
		clone[name] = append([]string(nil), values...)
	}
	return clone
}

var ErrBodyTooLarge = errors.New("request body exceeds configured limit")
