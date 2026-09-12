package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
)

type exchangeSummary struct {
	Transport         capture.Transport      `json:"transport,omitempty"`
	ID                string                 `json:"id"`
	Mode              string                 `json:"mode"`
	State             capture.State          `json:"state"`
	Revision          uint64                 `json:"revision"`
	ConfigRevision    uint64                 `json:"config_revision"`
	StartedAt         time.Time              `json:"started_at"`
	CompletedAt       *time.Time             `json:"completed_at,omitempty"`
	Method            string                 `json:"method"`
	Path              string                 `json:"path"`
	RequestBodyBytes  int                    `json:"request_body_bytes"`
	ResponseBodyBytes int64                  `json:"response_body_bytes"`
	ResponseStatus    int                    `json:"response_status,omitempty"`
	DurationNS        int64                  `json:"duration_ns"`
	RequestPreview    []byte                 `json:"request_preview,omitempty"`
	PreviewTruncated  bool                   `json:"preview_truncated"`
	Error             *capture.ExchangeError `json:"error,omitempty"`
	SourceAddress     string                 `json:"source_address,omitempty"`
	LocalAddress      string                 `json:"local_address,omitempty"`
	DatagramBytes     int64                  `json:"datagram_bytes,omitempty"`
	DiscardReason     string                 `json:"discard_reason,omitempty"`
}

type exchangeDetail struct {
	Exchange          capture.Exchange `json:"exchange"`
	RequestAvailable  *bool            `json:"request_available,omitempty"`
	DatagramAvailable *bool            `json:"datagram_available,omitempty"`
	ResponseAvailable bool             `json:"response_available"`
}

type listResponse struct {
	Items      []exchangeSummary `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more"`
}

func (api *handler) exchanges(writer http.ResponseWriter, request *http.Request) {
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(writer, http.StatusBadRequest, "invalid_limit", "Limit must be between 1 and 200.")
			return
		}
		limit = parsed
	}
	items := api.repository.List()
	var boundary *cursorBoundary
	if raw := request.URL.Query().Get("cursor"); raw != "" {
		cursor, err := decodeCursor(raw)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "invalid_cursor", "Cursor is invalid.")
			return
		}
		boundary = &cursor
		found := cursorIndex(items, cursor) >= 0
		if !found && api.persistent != nil {
			found, err = api.persistent.ContainsBoundary(request.Context(), sqliteBoundary(cursor))
			if err != nil {
				writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent history could not be read.")
				return
			}
		}
		if !found {
			writeError(writer, http.StatusConflict, "resync_required", "Cursor is no longer retained.")
			return
		}
	}
	items = olderThan(items, boundary)
	persistentMore := false
	if api.persistent != nil {
		persisted, hasMore, err := api.persistent.List(request.Context(), sqliteBoundaryPointer(boundary), limit+len(items))
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent history could not be read.")
			return
		}
		items = mergeExchanges(items, persisted)
		persistentMore = hasMore
	}
	hasMore := persistentMore || len(items) > limit
	if len(items) > limit {
		items = items[:limit]
	}
	page := items
	response := listResponse{Items: summarize(page), HasMore: hasMore}
	if response.HasMore && len(page) > 0 {
		last := page[len(page)-1]
		response.NextCursor = encodeCursor(exchangeOrderingTime(last), last.ID)
	}
	writeJSON(writer, http.StatusOK, response)
}

func olderThan(items []capture.Exchange, boundary *cursorBoundary) []capture.Exchange {
	if boundary == nil {
		return items
	}
	result := make([]capture.Exchange, 0, len(items))
	for _, exchange := range items {
		nanoseconds := exchangeOrderingTime(exchange).UnixNano()
		if nanoseconds < boundary.CompletedAt || nanoseconds == boundary.CompletedAt && exchange.ID < boundary.ID {
			result = append(result, exchange)
		}
	}
	return result
}

func sqliteBoundary(cursor cursorBoundary) requestsqlite.Boundary {
	return requestsqlite.Boundary{CompletedAt: time.Unix(0, cursor.CompletedAt).UTC(), ID: cursor.ID}
}

func sqliteBoundaryPointer(cursor *cursorBoundary) *requestsqlite.Boundary {
	if cursor == nil {
		return nil
	}
	boundary := sqliteBoundary(*cursor)
	return &boundary
}

func mergeExchanges(memory, persisted []capture.Exchange) []capture.Exchange {
	seen := make(map[string]struct{}, len(memory)+len(persisted))
	result := make([]capture.Exchange, 0, len(memory)+len(persisted))
	for _, source := range [][]capture.Exchange{memory, persisted} {
		for _, exchange := range source {
			if _, ok := seen[exchange.ID]; ok {
				continue
			}
			seen[exchange.ID] = struct{}{}
			result = append(result, exchange)
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		leftTime, rightTime := exchangeOrderingTime(result[left]), exchangeOrderingTime(result[right])
		if leftTime.Equal(rightTime) {
			return result[left].ID > result[right].ID
		}
		return leftTime.After(rightTime)
	})
	return result
}

func cursorIndex(items []capture.Exchange, cursor cursorBoundary) int {
	for index, exchange := range items {
		if exchange.ID == cursor.ID && exchangeOrderingTime(exchange).UnixNano() == cursor.CompletedAt {
			return index + 1
		}
	}
	return -1
}

func summarize(items []capture.Exchange) []exchangeSummary {
	result := make([]exchangeSummary, len(items))
	for index, exchange := range items {
		var completed *time.Time
		if !exchange.CompletedAt.IsZero() {
			value := exchange.CompletedAt
			completed = &value
		}
		requestBytes := len(exchange.Request.Body)
		if requestBytes == 0 && exchange.Request.ObservedBodyBytes > 0 {
			requestBytes = int(exchange.Request.ObservedBodyBytes)
		}
		responseBytes := int64(0)
		responseStatus := 0
		if exchange.Response != nil {
			responseBytes = int64(len(exchange.Response.Body))
			if responseBytes == 0 {
				responseBytes = exchange.Response.ObservedBodyBytes
			}
			responseStatus = exchange.Response.Status
		}
		summary := exchangeSummary{ID: exchange.ID, Transport: exchange.Transport, Mode: exchange.Mode, State: exchange.State, Revision: exchange.Revision, ConfigRevision: exchange.ConfigRevision, StartedAt: exchange.StartedAt, CompletedAt: completed, Method: exchange.Request.Method, Path: exchange.Request.Path, RequestBodyBytes: requestBytes, ResponseBodyBytes: responseBytes, ResponseStatus: responseStatus, DurationNS: int64(exchange.Duration), RequestPreview: append([]byte(nil), exchange.Request.Preview...), PreviewTruncated: exchange.Request.PreviewTruncated, Error: exchange.Error}
		if exchange.Transport == capture.TransportUDP && exchange.Datagram != nil {
			summary.SourceAddress, summary.LocalAddress, summary.DatagramBytes, summary.DiscardReason = exchange.Datagram.SourceAddress, exchange.Datagram.LocalAddress, exchange.Datagram.AcceptedBytes, exchange.Datagram.DiscardReason
			summary.Method, summary.Path, summary.RequestPreview = "", "", nil
		}
		result[index] = summary
	}
	return result
}

func exchangeOrderingTime(exchange capture.Exchange) time.Time {
	if !exchange.CompletedAt.IsZero() {
		return exchange.CompletedAt
	}
	return exchange.StartedAt
}

func (api *handler) exchange(writer http.ResponseWriter, request *http.Request) {
	exchange, ok, err := api.findExchange(request, request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent history could not be read.")
		return
	}
	if !ok {
		writeError(writer, http.StatusNotFound, "exchange_not_found", "Exchange does not exist.")
		return
	}
	requestAvailable := exchange.Request.BodyComplete
	detail := exchangeDetail{Exchange: exchange, ResponseAvailable: exchange.Response != nil && exchange.Response.BodyComplete}
	if exchange.Transport == capture.TransportUDP && exchange.Datagram != nil {
		datagramAvailable := exchange.Datagram.PayloadComplete
		detail.DatagramAvailable = &datagramAvailable
	} else {
		detail.RequestAvailable = &requestAvailable
	}
	exchange.Request.Body = nil
	if exchange.Response != nil {
		exchange.Response.Body = nil
	}
	detail.Exchange = exchange
	writeJSON(writer, http.StatusOK, detail)
}

func (api *handler) requestBody(writer http.ResponseWriter, request *http.Request) {
	exchange, ok, err := api.findExchange(request, request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent history could not be read.")
		return
	}
	if !ok {
		writeError(writer, http.StatusNotFound, "exchange_not_found", "Exchange does not exist.")
		return
	}
	if exchange.Transport == capture.TransportUDP {
		writeError(writer, http.StatusBadRequest, "transport_body_invalid", "UDP payloads use the datagram body route.")
		return
	}
	available := exchange.Request.BodyComplete
	if !available {
		writeError(writer, http.StatusConflict, "body_not_ready", "Request body is not available yet.")
		return
	}
	body := exchange.Request.Body
	filename := exchange.ID + "-request.bin"
	if _, inMemory := api.repository.Get(exchange.ID); !inMemory && api.persistent != nil {
		body, ok, err = api.persistent.Body(request.Context(), exchange.ID, false)
		if err != nil || !ok {
			writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent request body could not be read.")
			return
		}
	}
	writeBody(writer, request, filename, body)
}

func (api *handler) responseBody(writer http.ResponseWriter, request *http.Request) {
	exchange, ok, err := api.findExchange(request, request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent history could not be read.")
		return
	}
	if !ok {
		writeError(writer, http.StatusNotFound, "exchange_not_found", "Exchange does not exist.")
		return
	}
	if exchange.Transport == capture.TransportUDP {
		writeError(writer, http.StatusBadRequest, "transport_body_invalid", "UDP exchanges do not have an HTTP response body.")
		return
	}
	if exchange.Mode == config.ModeCapture {
		writeError(writer, http.StatusNotFound, "response_not_captured", "Capture responses are not inspected.")
		return
	}
	if exchange.Response == nil || !exchange.Response.BodyComplete {
		writeError(writer, http.StatusConflict, "body_not_ready", "Response body is not available yet.")
		return
	}
	body := exchange.Response.Body
	if _, inMemory := api.repository.Get(exchange.ID); !inMemory && api.persistent != nil {
		body, ok, err = api.persistent.Body(request.Context(), exchange.ID, true)
		if err != nil || !ok {
			writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent response body could not be read.")
			return
		}
	}
	writeBody(writer, request, exchange.ID+"-response.bin", body)
}

func (api *handler) datagramBody(writer http.ResponseWriter, request *http.Request) {
	exchange, ok, err := api.findExchange(request, request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent history could not be read.")
		return
	}
	if !ok {
		writeError(writer, http.StatusNotFound, "exchange_not_found", "Exchange does not exist.")
		return
	}
	if exchange.Transport != capture.TransportUDP || exchange.Datagram == nil {
		writeError(writer, http.StatusBadRequest, "transport_body_invalid", "HTTP exchanges do not have a UDP datagram payload.")
		return
	}
	if !exchange.Datagram.PayloadComplete {
		writeError(writer, http.StatusConflict, "body_not_ready", "UDP datagram payload is not available yet.")
		return
	}
	body := exchange.Datagram.Payload
	if _, inMemory := api.repository.Get(exchange.ID); !inMemory && api.persistent != nil {
		body, ok, err = api.persistent.Body(request.Context(), exchange.ID, false)
		if err != nil || !ok {
			writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Persistent UDP datagram payload could not be read.")
			return
		}
	}
	writeBody(writer, request, exchange.ID+"-udp-datagram.bin", body)
}

func (api *handler) findExchange(request *http.Request, id string) (capture.Exchange, bool, error) {
	if exchange, ok := api.repository.Get(id); ok {
		return exchange, true, nil
	}
	if api.persistent == nil {
		return capture.Exchange{}, false, nil
	}
	return api.persistent.Get(request.Context(), id)
}

func writeBody(writer http.ResponseWriter, request *http.Request, filename string, body []byte) {
	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeFilename(filename)))
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if len(body) == 0 {
		if request.Header.Get("Range") != "" {
			writer.Header().Set("Content-Range", "bytes */0")
			writeError(writer, http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable", "Requested body range is invalid.")
			return
		}
		writer.Header().Set("Content-Length", "0")
		writer.WriteHeader(http.StatusOK)
		return
	}
	selected := byteRange{start: 0, end: int64(len(body)) - 1}
	status := http.StatusOK
	if raw := request.Header.Get("Range"); raw != "" {
		var err error
		selected, err = parseRange(raw, int64(len(body)))
		if err != nil {
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(body)))
			writeError(writer, http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable", "Requested body range is invalid.")
			return
		}
		status = http.StatusPartialContent
		writer.Header().Set("Content-Range", contentRange(selected, int64(len(body))))
	}
	data := body[selected.start : selected.end+1]
	writer.Header().Set("Content-Length", strconv.Itoa(len(data)))
	writer.WriteHeader(status)
	if _, err := writer.Write(data); err != nil {
		return
	}
}

func safeFilename(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '.' {
			return character
		}
		return '_'
	}, value)
}

func (api *handler) clearExchanges(writer http.ResponseWriter, request *http.Request) {
	if api.persistent != nil {
		if err := api.persistent.WaitIdle(request.Context()); err != nil {
			writeError(writer, http.StatusInternalServerError, "storage_delete_failed", "Persistent history could not be cleared.")
			return
		}
		if _, err := api.persistent.Clear(request.Context()); err != nil {
			writeError(writer, http.StatusInternalServerError, "storage_delete_failed", "Persistent history could not be cleared.")
			return
		}
	}
	removed := api.repository.Clear()
	api.events.Publish(events.Envelope{Type: events.HistoryCleared, Data: map[string]int{"removed": len(removed)}})
	writeJSON(writer, http.StatusOK, map[string]int{"removed": len(removed)})
}

func (api *handler) deleteExchange(writer http.ResponseWriter, request *http.Request) {
	exchange, ok, err := api.findExchange(request, request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "storage_read_failed", "Exchange could not be read.")
		return
	}
	if !ok {
		writeError(writer, http.StatusNotFound, "exchange_not_found", "Exchange does not exist.")
		return
	}
	if !capture.IsTerminal(exchange.State) {
		writeError(writer, http.StatusConflict, "exchange_active", "Active exchange cannot be deleted.")
		return
	}
	if api.persistent != nil {
		if err := api.persistent.WaitIdle(request.Context()); err != nil {
			writeError(writer, http.StatusInternalServerError, "storage_delete_failed", "Exchange could not be deleted from persistent history.")
			return
		}
		if _, err := api.persistent.Delete(request.Context(), exchange.ID); err != nil {
			writeError(writer, http.StatusInternalServerError, "storage_delete_failed", "Exchange could not be deleted from persistent history.")
			return
		}
	}
	api.repository.Remove(exchange.ID)
	api.events.Publish(events.Envelope{Type: events.ExchangeDeleted, ExchangeID: exchange.ID, Revision: exchange.Revision})
	writeJSON(writer, http.StatusOK, map[string]bool{"deleted": true})
}
