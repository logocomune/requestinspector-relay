package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
)

type Repository interface {
	Save(Exchange) []Exchange
}

type PersistenceReservation interface {
	Commit(Exchange) error
	Cancel()
}

type ReservingRepository interface {
	Reserve(int64) (PersistenceReservation, bool)
}

type Publisher interface {
	Publish(events.Envelope) events.Envelope
}

type ModeHandler interface {
	Handle(context.Context, Request, ConfigSnapshot) (ModeResult, error)
}

type ModeResult struct {
	Response                            DownstreamResponse
	CapturedResponse                    *Response
	EffectiveUpstream                   string
	ProxyTiming                         *ProxyTiming
	RequestInspection                   *Inspection
	UpstreamResponseHeaderBytesEstimate int64
	UpstreamObservedResponseBodyBytes   int64
}

type Inspection struct {
	Preview             []byte
	PreviewTruncated    bool
	DecodedPreviewBytes *int64
	Error               string
}

type ModeError struct {
	Status   int
	Category string
	Message  string
	Cause    error
}

func (failure *ModeError) Error() string {
	if failure.Cause != nil {
		return failure.Cause.Error()
	}
	return failure.Message
}

func (failure *ModeError) Unwrap() error { return failure.Cause }

type DownstreamResponse struct {
	Status  int
	Headers http.Header
	Body    []byte
}

type PipelineOptions struct {
	Snapshot   func() ConfigSnapshot
	Repository Repository
	Publisher  Publisher
	Handlers   map[string]ModeHandler
	ID         func() (string, error)
	Now        func() time.Time
}

type Pipeline struct {
	snapshot          func() ConfigSnapshot
	repository        Repository
	publisher         Publisher
	handlers          map[string]ModeHandler
	id                func() (string, error)
	now               func() time.Time
	admissionMutex    sync.Mutex
	inFlight          int
	admissionRejected uint64
	bodyTooLarge      uint64
}

type Metrics struct {
	CurrentRequests   int
	AdmissionRejected uint64
	BodyTooLarge      uint64
}

func NewPipeline(options PipelineOptions) *Pipeline {
	return &Pipeline{
		snapshot:   options.Snapshot,
		repository: options.Repository,
		publisher:  options.Publisher,
		handlers:   options.Handlers,
		id:         options.ID,
		now:        options.Now,
	}
}

func (pipeline *Pipeline) ServeHTTP(writer http.ResponseWriter, incoming *http.Request) {
	snapshot := pipeline.snapshot().Clone()
	if !pipeline.admit(snapshot.Limits.ConcurrentExchanges) {
		writePipelineError(writer, http.StatusServiceUnavailable, "capacity_exhausted", "Concurrent exchange capacity is exhausted.")
		return
	}
	defer pipeline.release()
	reservation, admitted := pipeline.reservePersistence(snapshot)
	if !admitted {
		pipeline.rejectAdmission()
		writePipelineError(writer, http.StatusServiceUnavailable, "persistence_capacity_exhausted", "Persistence writer capacity is exhausted.")
		return
	}
	if reservation != nil {
		defer reservation.Cancel()
	}

	exchange, err := pipeline.startExchange(incoming, snapshot)
	if err != nil {
		writePipelineError(writer, http.StatusInternalServerError, "exchange_id_failed", "Exchange could not be created.")
		return
	}
	if reservation != nil {
		defer func() {
			if IsTerminal(exchange.State) {
				exchange.Persistence = &PersistenceStatus{State: "queued"}
				pipeline.save(&exchange)
				if err := reservation.Commit(exchange); err != nil {
					exchange.Persistence = &PersistenceStatus{State: "failed", Error: err.Error()}
					pipeline.save(&exchange)
				}
			}
		}()
	}
	if exchange.Request.HeaderBytesEstimate > int64(snapshot.Limits.RequestHeaderBytes) {
		exchange.Error = &ExchangeError{Category: "request_headers_too_large", Message: "Request headers exceed configured limit."}
		pipeline.fail(&exchange, StateFailed)
		writePipelineError(writer, http.StatusRequestHeaderFieldsTooLarge, exchange.Error.Category, exchange.Error.Message)
		return
	}

	body, observed, readError := ReadBody(incoming.Body, snapshot.Limits.RequestBodyBytes)
	exchange.Request.Body = body
	exchange.Request.ObservedBodyBytes = observed
	exchange.Request.BodyComplete = readError == nil
	exchange.Request.Preview, exchange.Request.PreviewTruncated = preview(body, snapshot.Preview)
	if readError != nil {
		category := "request_body_read_failed"
		status := http.StatusBadRequest
		message := "Request body could not be read."
		if errors.Is(readError, ErrBodyTooLarge) {
			pipeline.recordBodyTooLarge()
			category = "request_body_too_large"
			status = http.StatusRequestEntityTooLarge
			message = "Request body exceeds configured limit."
		}
		exchange.Error = &ExchangeError{Category: category, Message: message}
		pipeline.fail(&exchange, StateFailed)
		writePipelineError(writer, status, category, message)
		return
	}

	exchange.Revision++
	pipeline.save(&exchange)
	pipeline.publisher.Publish(events.Envelope{
		Type:       events.RequestCompleted,
		ExchangeID: exchange.ID,
		Revision:   exchange.Revision,
		Data: map[string]any{
			"request_header_bytes_estimate": exchange.Request.HeaderBytesEstimate,
			"request_body_bytes":            len(exchange.Request.Body),
			"preview":                       exchange.Request.Preview,
			"preview_truncated":             exchange.Request.PreviewTruncated,
		},
	})

	if snapshot.Mode == config.ModeProxy {
		if err := exchange.Transition(StateForwarding, pipeline.now()); err != nil {
			pipeline.internalFailure(writer, &exchange, err)
			return
		}
		pipeline.save(&exchange)
	}
	handler, ok := pipeline.handlers[snapshot.Mode]
	if !ok {
		exchange.Error = &ExchangeError{Category: "mode_handler_unavailable", Message: "Configured mode is unavailable."}
		pipeline.fail(&exchange, StateFailed)
		writePipelineError(writer, http.StatusNotImplemented, exchange.Error.Category, exchange.Error.Message)
		return
	}
	result, handleError := handler.Handle(incoming.Context(), exchange.Request.Clone(), snapshot.Clone())
	pipeline.applyModeResult(&exchange, result)
	if handleError != nil {
		state, status, category, message := modeFailure(handleError, incoming.Context())
		exchange.Error = &ExchangeError{Category: category, Message: message}
		response := pipelineErrorResponse(status, category, message)
		if snapshot.Mode == config.ModeProxy && state != StateCancelled {
			exchange.Response = capturedProxyError(response, snapshot.Preview)
		}
		written, writeError := pipeline.writeResponse(writer, &exchange, response)
		exchange.DownstreamDelivery = DownstreamDelivery{Status: status, BodyBytes: written}
		if writeError != nil {
			exchange.DownstreamDelivery.Error = writeError.Error()
		}
		pipeline.fail(&exchange, state)
		return
	}
	if snapshot.Mode == config.ModeProxy && exchange.Response != nil && exchange.Response.Origin == "upstream" {
		exchange.Revision++
		pipeline.save(&exchange)
		pipeline.publisher.Publish(events.Envelope{
			Type:       events.ResponseCompleted,
			ExchangeID: exchange.ID,
			Revision:   exchange.Revision,
			Data: map[string]any{
				"response_header_bytes_estimate": exchange.Response.HeaderBytesEstimate,
				"response_body_bytes":            len(exchange.Response.Body),
				"proxy_timing":                   cloneProxyTiming(exchange.ProxyTiming),
			},
		})
	}
	written, writeError := pipeline.writeResponse(writer, &exchange, result.Response)
	exchange.DownstreamDelivery.Status = normalizedStatus(result.Response.Status)
	exchange.DownstreamDelivery.BodyBytes = written
	if writeError != nil {
		exchange.DownstreamDelivery.Error = writeError.Error()
		exchange.Error = &ExchangeError{Category: "downstream_write_failed", Message: "Response could not be delivered completely."}
		pipeline.fail(&exchange, StateFailed)
		return
	}
	if err := exchange.Transition(StateCompleted, pipeline.now()); err != nil {
		pipeline.internalFailure(writer, &exchange, err)
		return
	}
	pipeline.save(&exchange)
}

func (pipeline *Pipeline) reservePersistence(snapshot ConfigSnapshot) (PersistenceReservation, bool) {
	repository, ok := pipeline.repository.(ReservingRepository)
	if !ok {
		return nil, true
	}
	bytes := reservationBytes(0, snapshot.Limits.RequestBodyBytes)
	bytes = reservationBytes(bytes, int64(snapshot.Limits.RequestHeaderBytes))
	if snapshot.Mode == config.ModeProxy {
		bytes = reservationBytes(bytes, snapshot.Limits.ResponseBodyBytes)
		bytes = reservationBytes(bytes, int64(snapshot.Limits.ResponseHeaderBytes))
	}
	return repository.Reserve(bytes)
}

func reservationBytes(total, value int64) int64 {
	if value <= 0 {
		return total
	}
	const maximum = int64(^uint64(0) >> 1)
	if total > maximum-value {
		return maximum
	}
	return total + value
}

func (pipeline *Pipeline) applyModeResult(exchange *Exchange, result ModeResult) {
	exchange.Response = cloneResponse(result.CapturedResponse)
	exchange.EffectiveUpstream = result.EffectiveUpstream
	exchange.ProxyTiming = cloneProxyTiming(result.ProxyTiming)
	exchange.UpstreamResponseHeaderBytesEstimate = result.UpstreamResponseHeaderBytesEstimate
	exchange.UpstreamObservedResponseBodyBytes = result.UpstreamObservedResponseBodyBytes
	if result.RequestInspection == nil {
		return
	}
	exchange.Request.Preview = append([]byte(nil), result.RequestInspection.Preview...)
	exchange.Request.PreviewTruncated = result.RequestInspection.PreviewTruncated
	exchange.Request.DecodedPreviewBytes = cloneInt64(result.RequestInspection.DecodedPreviewBytes)
	exchange.Request.PreviewError = result.RequestInspection.Error
}

func modeFailure(err error, requestContext context.Context) (State, int, string, string) {
	if errors.Is(err, context.Canceled) || errors.Is(requestContext.Err(), context.Canceled) {
		return StateCancelled, http.StatusBadGateway, "request_cancelled", "Request was cancelled."
	}
	var failure *ModeError
	if errors.As(err, &failure) {
		return StateFailed, normalizedStatus(failure.Status), failure.Category, failure.Message
	}
	return StateFailed, http.StatusBadGateway, "mode_handler_failed", "Request processing failed."
}

func capturedProxyError(response DownstreamResponse, settings config.Preview) *Response {
	previewBody, truncated := preview(response.Body, settings)
	return &Response{
		Status: response.Status, Headers: response.Headers.Clone(), Body: append([]byte(nil), response.Body...),
		ObservedBodyBytes: int64(len(response.Body)), BodyComplete: true,
		HeaderBytesEstimate: HeaderBytesEstimate(response.Headers, ""), Origin: "proxy_error",
		Preview: previewBody, PreviewTruncated: truncated,
	}
}

func pipelineErrorResponse(status int, code, message string) DownstreamResponse {
	body, err := json.Marshal(map[string]string{"code": code, "message": message})
	if err != nil {
		body = []byte(`{"code":"internal_error","message":"Request processing failed."}`)
	}
	body = append(body, '\n')
	return DownstreamResponse{Status: status, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body}
}

func (pipeline *Pipeline) writeResponse(writer http.ResponseWriter, exchange *Exchange, response DownstreamResponse) (int64, error) {
	started := pipeline.now()
	written, err := writeDownstream(writer, response)
	completed := pipeline.now()
	if exchange.Mode == config.ModeProxy {
		if exchange.ProxyTiming == nil {
			exchange.ProxyTiming = &ProxyTiming{}
		}
		completedUTC := completed.UTC()
		exchange.ProxyTiming.DownstreamCompletedAt = &completedUTC
		exchange.ProxyTiming.DownstreamWriteDurationUS = durationMicroseconds(started, completed)
		exchange.ProxyTiming.ProxyTotalDurationUS = durationMicroseconds(exchange.startedMonotonic, completed)
	}
	return written, err
}

func durationMicroseconds(started, completed time.Time) *int64 {
	if started.IsZero() || completed.Before(started) {
		return nil
	}
	value := completed.Sub(started).Microseconds()
	return &value
}

func (pipeline *Pipeline) admit(limit int) bool {
	if limit < 1 {
		limit = 1
	}
	pipeline.admissionMutex.Lock()
	defer pipeline.admissionMutex.Unlock()
	if pipeline.inFlight >= limit {
		pipeline.admissionRejected++
		return false
	}
	pipeline.inFlight++
	return true
}

func (pipeline *Pipeline) release() {
	pipeline.admissionMutex.Lock()
	pipeline.inFlight--
	pipeline.admissionMutex.Unlock()
}

func (pipeline *Pipeline) rejectAdmission() {
	pipeline.admissionMutex.Lock()
	pipeline.admissionRejected++
	pipeline.admissionMutex.Unlock()
}

func (pipeline *Pipeline) recordBodyTooLarge() {
	pipeline.admissionMutex.Lock()
	pipeline.bodyTooLarge++
	pipeline.admissionMutex.Unlock()
}

func (pipeline *Pipeline) Metrics() Metrics {
	pipeline.admissionMutex.Lock()
	defer pipeline.admissionMutex.Unlock()
	return Metrics{
		CurrentRequests:   pipeline.inFlight,
		AdmissionRejected: pipeline.admissionRejected,
		BodyTooLarge:      pipeline.bodyTooLarge,
	}
}

func (pipeline *Pipeline) startExchange(incoming *http.Request, snapshot ConfigSnapshot) (Exchange, error) {
	startedMonotonic := pipeline.now()
	id, err := pipeline.id()
	if err != nil {
		return Exchange{}, err
	}
	started := startedMonotonic.UTC()
	exchange := Exchange{
		ID:               id,
		Transport:        "http",
		Mode:             snapshot.Mode,
		ConfigRevision:   snapshot.Revision,
		Configuration:    snapshot,
		Revision:         1,
		State:            StateReceiving,
		StartedAt:        started,
		startedMonotonic: startedMonotonic,
		Request: Request{
			Method:              incoming.Method,
			Scheme:              requestScheme(incoming),
			Protocol:            incoming.Proto,
			Host:                incoming.Host,
			RequestTarget:       incoming.RequestURI,
			Path:                incoming.URL.EscapedPath(),
			RawQuery:            incoming.URL.RawQuery,
			RemoteAddress:       incoming.RemoteAddr,
			Headers:             incoming.Header.Clone(),
			ContentType:         incoming.Header.Get("Content-Type"),
			HeaderBytesEstimate: HeaderBytesEstimate(incoming.Header, incoming.Host),
		},
	}
	pipeline.save(&exchange)
	pipeline.publisher.Publish(events.Envelope{Type: events.ExchangeStarted, ExchangeID: exchange.ID, Revision: exchange.Revision})
	return exchange, nil
}

func requestScheme(request *http.Request) string {
	if request.TLS != nil {
		return "https"
	}
	return "http"
}

func (pipeline *Pipeline) fail(exchange *Exchange, state State) {
	if err := exchange.Transition(state, pipeline.now()); err != nil {
		exchange.State = StateFailed
	}
	pipeline.save(exchange)
	pipeline.publisher.Publish(events.Envelope{
		Type:       events.ExchangeFailed,
		ExchangeID: exchange.ID,
		Revision:   exchange.Revision,
		Data:       exchange.Error,
	})
}

func (pipeline *Pipeline) internalFailure(writer http.ResponseWriter, exchange *Exchange, cause error) {
	exchange.Error = &ExchangeError{Category: "pipeline_state_failed", Message: cause.Error()}
	pipeline.fail(exchange, StateFailed)
	writePipelineError(writer, http.StatusInternalServerError, exchange.Error.Category, "Request state could not be updated.")
}

func (pipeline *Pipeline) save(exchange *Exchange) {
	for _, evicted := range pipeline.repository.Save(*exchange) {
		pipeline.publisher.Publish(events.Envelope{Type: events.ExchangeEvicted, ExchangeID: evicted.ID, Revision: evicted.Revision})
	}
}

func preview(body []byte, settings config.Preview) ([]byte, bool) {
	if !settings.Enabled || int64(len(body)) <= settings.Bytes {
		return append([]byte(nil), body...), false
	}
	return append([]byte(nil), body[:settings.Bytes]...), true
}

func writeDownstream(writer http.ResponseWriter, response DownstreamResponse) (int64, error) {
	for name, values := range response.Headers {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	status := normalizedStatus(response.Status)
	writer.WriteHeader(status)
	if len(response.Body) == 0 || status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified || status >= 100 && status < 200 {
		return 0, nil
	}
	written, err := writer.Write(response.Body)
	if err != nil {
		return int64(written), fmt.Errorf("write downstream response: %w", err)
	}
	return int64(written), nil
}

func normalizedStatus(status int) int {
	if status < 100 || status > 599 {
		return http.StatusOK
	}
	return status
}

func cloneResponse(response *Response) *Response {
	if response == nil {
		return nil
	}
	clone := *response
	clone.Headers = response.Headers.Clone()
	clone.Body = append([]byte(nil), response.Body...)
	clone.Preview = append([]byte(nil), response.Preview...)
	clone.DecodedPreviewBytes = cloneInt64(response.DecodedPreviewBytes)
	return &clone
}

func writePipelineError(writer http.ResponseWriter, status int, code, message string) {
	_, _ = writeDownstream(writer, pipelineErrorResponse(status, code, message))
}
