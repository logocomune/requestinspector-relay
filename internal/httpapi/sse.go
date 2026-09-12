package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/events"
)

type streamSnapshot struct {
	SchemaVersion int               `json:"schema_version"`
	EventCursor   string            `json:"event_cursor"`
	Items         []exchangeSummary `json:"items"`
}

type streamResync struct {
	SchemaVersion int               `json:"schema_version"`
	Reason        string            `json:"reason"`
	EventCursor   string            `json:"event_cursor"`
	Items         []exchangeSummary `json:"items"`
}

func (api *handler) eventStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "streaming_unsupported", "Streaming is unavailable.")
		return
	}
	if err := http.NewResponseController(writer).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(writer, http.StatusInternalServerError, "streaming_unavailable", "Streaming deadline could not be configured.")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	subscription := api.openSubscription(writer, request.Header.Get("Last-Event-ID"))
	defer subscription.Close()
	flusher.Flush()
	heartbeat := time.NewTicker(api.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-api.shutdown:
			return
		case <-request.Context().Done():
			return
		case envelope, open := <-subscription.Events:
			if !open {
				return
			}
			writeSSE(writer, envelope.ID, string(envelope.Type), envelope)
			flusher.Flush()
		case <-heartbeat.C:
			if !api.auth.valid(request) {
				return
			}
			if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (api *handler) openSubscription(writer io.Writer, lastID string) *events.Subscription {
	if lastID == "" {
		result, items := api.subscribeWithSnapshot()
		writeSSE(writer, result.Cursor, "snapshot", streamSnapshot{SchemaVersion: events.SchemaVersion, EventCursor: result.Cursor, Items: items})
		return result.Subscription
	}
	subscription := api.events.Subscribe(lastID)
	if subscription.Reset {
		subscription.Close()
		result, items := api.subscribeWithSnapshot()
		writeSSE(writer, result.Cursor, "resync", streamResync{SchemaVersion: events.SchemaVersion, Reason: "replay_gap", EventCursor: result.Cursor, Items: items})
		return result.Subscription
	}
	for _, envelope := range subscription.Replay {
		writeSSE(writer, envelope.ID, string(envelope.Type), envelope)
	}
	return subscription
}

func (api *handler) subscribeWithSnapshot() (events.SnapshotSubscription, []exchangeSummary) {
	var items []exchangeSummary
	result := api.events.SubscribeWithSnapshot(func() any {
		items = summarize(api.repository.List())
		return items
	})
	return result, items
}

func writeSSE(writer io.Writer, id, event string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	if id != "" {
		if _, err := fmt.Fprintf(writer, "id: %s\n", id); err != nil {
			return
		}
	}
	if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return
	}
}
