package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
	"github.com/logocomune/requestinspector-relay/internal/store"
)

func TestSQLiteHistoryAPIReadsLazyPagesDetailsAndBodies(t *testing.T) {
	api, memory, persistent := sqliteTestHandler(t)
	for index, id := range []string{"old", "middle", "new"} {
		exchange := apiExchange(id, time.Unix(int64(index+1), 0), true)
		exchange.Request.ObservedBodyBytes = int64(len(exchange.Request.Body))
		persistAPIExchange(t, persistent, exchange)
	}
	memory.Clear()
	first := perform(t, api, http.MethodGet, "/api/v1/exchanges?limit=2", nil, nil)
	var page listResponse
	decodeRecorder(t, first, &page)
	if first.Code != http.StatusOK || len(page.Items) != 2 || page.Items[0].ID != "new" || page.Items[0].RequestBodyBytes != 6 || !page.HasMore {
		t.Fatalf("first page status=%d page=%+v", first.Code, page)
	}

	newer := apiExchange("latest", time.Unix(4, 0), true)
	newer.Request.ObservedBodyBytes = int64(len(newer.Request.Body))
	persistAPIExchange(t, persistent, newer)
	second := perform(t, api, http.MethodGet, "/api/v1/exchanges?limit=2&cursor="+url.QueryEscape(page.NextCursor), nil, nil)
	decodeRecorder(t, second, &page)
	if second.Code != http.StatusOK || len(page.Items) != 1 || page.Items[0].ID != "old" || page.HasMore {
		t.Fatalf("stable older page status=%d page=%+v", second.Code, page)
	}

	detail := perform(t, api, http.MethodGet, "/api/v1/exchanges/new", nil, nil)
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), "YWJjZGVm") {
		t.Fatalf("lazy detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	body := perform(t, api, http.MethodGet, "/api/v1/exchanges/new/request/body", nil, map[string]string{"Range": "bytes=2-4"})
	if body.Code != http.StatusPartialContent || body.Body.String() != "cde" {
		t.Fatalf("body status=%d value=%q", body.Code, body.Body.String())
	}
}

func TestSQLiteHistoryAPIReadsUDPPayloadRange(t *testing.T) {
	api, memory, persistent := sqliteTestHandler(t)
	udp := capture.Exchange{
		ID: "udp", Transport: capture.TransportUDP, Mode: config.ModeCapture, State: capture.StateCompleted, StartedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0),
		Datagram: &capture.Datagram{SourceAddress: "127.0.0.1:10001", LocalAddress: "127.0.0.1:9000", AcceptedBytes: 6, Payload: []byte("abcdef"), PayloadComplete: true},
	}
	persistAPIExchange(t, persistent, udp)
	memory.Clear()

	detail := perform(t, api, http.MethodGet, "/api/v1/exchanges/udp", nil, nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"transport":"udp"`) || !strings.Contains(detail.Body.String(), `"datagram_available":true`) || strings.Contains(detail.Body.String(), `"request_available"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	body := perform(t, api, http.MethodGet, "/api/v1/exchanges/udp/datagram/body", nil, map[string]string{"Range": "bytes=2-4"})
	if body.Code != http.StatusPartialContent || body.Body.String() != "cde" || !strings.Contains(body.Header().Get("Content-Disposition"), "udp-datagram.bin") {
		t.Fatalf("body status=%d headers=%v value=%q", body.Code, body.Header(), body.Body.String())
	}
}

func TestSQLiteHistoryAPIDeletesOnePersistentExchange(t *testing.T) {
	api, memory, persistent := sqliteTestHandler(t)
	persistAPIExchange(t, persistent, apiExchange("remove", time.Unix(1, 0), true))
	persistAPIExchange(t, persistent, apiExchange("keep", time.Unix(2, 0), true))
	memory.Clear()

	deleted := perform(t, api, http.MethodDelete, "/api/v1/exchanges/remove", nil, nil)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"deleted":true`) {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/remove", nil, nil), http.StatusNotFound, "exchange_not_found")
	if kept := perform(t, api, http.MethodGet, "/api/v1/exchanges/keep", nil, nil); kept.Code != http.StatusOK {
		t.Fatalf("kept status=%d body=%s", kept.Code, kept.Body.String())
	}
}

func TestSQLiteMaintenanceAPIStatusPreviewCleanupCompactAndVacuum(t *testing.T) {
	api, memory, persistent := sqliteTestHandler(t)
	exchange := apiExchange("old", time.Now().Add(-48*time.Hour), true)
	persistAPIExchange(t, persistent, exchange)
	memory.Clear()
	status := perform(t, api, http.MethodGet, "/api/v1/storage/sqlite", nil, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"exchange_count":1`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
	preview := perform(t, api, http.MethodGet, "/api/v1/storage/sqlite/cleanup-preview?keep_days=1", nil, nil)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"exchange_count":1`) {
		t.Fatalf("preview=%d body=%s", preview.Code, preview.Body.String())
	}
	assertErrorCode(t, perform(t, api, http.MethodPost, "/api/v1/storage/sqlite/cleanup", []byte(`{"keep_days":1}`), nil), http.StatusPreconditionFailed, "confirmation_required")
	cleanup := perform(t, api, http.MethodPost, "/api/v1/storage/sqlite/cleanup", []byte(`{"keep_days":1,"confirm":true}`), nil)
	if cleanup.Code != http.StatusOK || !strings.Contains(cleanup.Body.String(), `"deleted":1`) {
		t.Fatalf("cleanup=%d body=%s", cleanup.Code, cleanup.Body.String())
	}
	if compact := perform(t, api, http.MethodPost, "/api/v1/storage/sqlite/compact", nil, nil); compact.Code != http.StatusOK {
		t.Fatalf("compact=%d body=%s", compact.Code, compact.Body.String())
	}
	assertErrorCode(t, perform(t, api, http.MethodPost, "/api/v1/storage/sqlite/vacuum", []byte(`{"confirm":false}`), nil), http.StatusPreconditionFailed, "confirmation_required")
	if vacuum := perform(t, api, http.MethodPost, "/api/v1/storage/sqlite/vacuum", []byte(`{"confirm":true}`), nil); vacuum.Code != http.StatusOK {
		t.Fatalf("vacuum=%d body=%s", vacuum.Code, vacuum.Body.String())
	}
}

func TestSQLiteAPIRejectsInvalidInputsAndExpiredCursor(t *testing.T) {
	api, _, _ := sqliteTestHandler(t)
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/storage/sqlite/cleanup-preview?keep_days=-1", nil, nil), http.StatusBadRequest, "invalid_keep_days")
	assertErrorCode(t, perform(t, api, http.MethodPost, "/api/v1/storage/sqlite/cleanup", []byte(`{"keep_days":-1,"confirm":true}`), nil), http.StatusBadRequest, "invalid_cleanup")
	expired := encodeCursor(time.Unix(1, 0), "gone")
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges?cursor="+expired, nil, nil), http.StatusConflict, "resync_required")
}

func sqliteTestHandler(t *testing.T) (http.Handler, *store.Memory, *requestsqlite.Repository) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.SQLitePath = filepath.Join(t.TempDir(), "history.db")
	manager, err := config.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	memory := store.NewMemory(cfg.History.MaxExchanges)
	persistent, err := requestsqlite.Open(requestsqlite.Options{Path: cfg.Storage.SQLitePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = persistent.Close(context.Background()) })
	bus := events.NewBus(events.Limits{ReplayCount: 10, ReplayBytes: 1 << 20, SubscriberCount: 2, SubscriberBytes: 1 << 20}, time.Now)
	api := NewManagementHandler(Options{Config: manager, Repository: memory, Persistent: persistent, Events: bus, Build: "test", Started: time.Now(), Static: http.NotFoundHandler()})
	return api, memory, persistent
}

func persistAPIExchange(t *testing.T, persistent *requestsqlite.Repository, exchange capture.Exchange) {
	t.Helper()
	reservation, ok := persistent.Reserve(1)
	if !ok {
		t.Fatal("persistence reservation rejected")
	}
	if err := reservation.Commit(exchange); err != nil {
		t.Fatal(err)
	}
	if err := persistent.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}
