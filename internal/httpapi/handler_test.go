package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	"github.com/logocomune/requestinspector-relay/internal/store"
)

func TestExchangeAPIListsPagesDetailsRangesAndClears(t *testing.T) {
	api, repository, _ := testHandler(t, config.Defaults(), time.Now)
	for index, id := range []string{"old", "middle", "new"} {
		repository.Save(apiExchange(id, time.Unix(int64(index+1), 0), true))
	}

	first := perform(t, api, http.MethodGet, "/api/v1/exchanges?limit=2", nil, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d body=%s", first.Code, first.Body.String())
	}
	var page listResponse
	decodeRecorder(t, first, &page)
	if len(page.Items) != 2 || page.Items[0].ID != "new" || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("first page = %+v", page)
	}
	second := perform(t, api, http.MethodGet, "/api/v1/exchanges?limit=2&cursor="+url.QueryEscape(page.NextCursor), nil, nil)
	decodeRecorder(t, second, &page)
	if len(page.Items) != 1 || page.Items[0].ID != "old" || page.HasMore {
		t.Fatalf("second page = %+v", page)
	}

	detail := perform(t, api, http.MethodGet, "/api/v1/exchanges/new", nil, nil)
	if strings.Contains(detail.Body.String(), "configured capture response") || strings.Contains(detail.Body.String(), `"Body"`) {
		t.Fatalf("detail exposed generated or full body: %s", detail.Body.String())
	}
	responseBody := perform(t, api, http.MethodGet, "/api/v1/exchanges/new/response/body", nil, nil)
	assertErrorCode(t, responseBody, http.StatusNotFound, "response_not_captured")
	ranged := perform(t, api, http.MethodGet, "/api/v1/exchanges/new/request/body", nil, map[string]string{"Range": "bytes=1-3"})
	if ranged.Code != http.StatusPartialContent || ranged.Body.String() != "bcd" || ranged.Header().Get("Content-Range") != "bytes 1-3/6" || ranged.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(ranged.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("range response status=%d headers=%v body=%q", ranged.Code, ranged.Header(), ranged.Body.String())
	}
	invalidRange := perform(t, api, http.MethodGet, "/api/v1/exchanges/new/request/body", nil, map[string]string{"Range": "bytes=99-100"})
	assertErrorCode(t, invalidRange, http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable")

	cleared := perform(t, api, http.MethodDelete, "/api/v1/exchanges", nil, nil)
	if cleared.Code != http.StatusOK || len(repository.List()) != 0 {
		t.Fatalf("clear status=%d remaining=%d", cleared.Code, len(repository.List()))
	}
}

func TestExchangeAPIReportsMissingNotReadyAndExpiredCursor(t *testing.T) {
	api, repository, _ := testHandler(t, config.Defaults(), time.Now)
	active := apiExchange("active", time.Now(), false)
	active.State = capture.StateReceiving
	repository.Save(active)
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/missing", nil, nil), http.StatusNotFound, "exchange_not_found")
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/active/request/body", nil, nil), http.StatusConflict, "body_not_ready")
	expired := encodeCursor(time.Unix(1, 0), "evicted")
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges?cursor="+expired, nil, nil), http.StatusConflict, "resync_required")
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges?cursor=broken", nil, nil), http.StatusBadRequest, "invalid_cursor")
}

func TestExchangeAPIDeletesCompletedExchangeAndPublishesEviction(t *testing.T) {
	api, repository, bus := testHandler(t, config.Defaults(), time.Now)
	completed := apiExchange("completed", time.Now(), true)
	repository.Save(completed)
	active := apiExchange("active", time.Now(), false)
	active.State = capture.StateReceiving
	repository.Save(active)

	deleted := perform(t, api, http.MethodDelete, "/api/v1/exchanges/completed", nil, nil)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"deleted":true`) {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, found := repository.Get(completed.ID); found {
		t.Fatal("deleted exchange remains in memory")
	}
	assertErrorCode(t, perform(t, api, http.MethodDelete, "/api/v1/exchanges/active", nil, nil), http.StatusConflict, "exchange_active")
	assertErrorCode(t, perform(t, api, http.MethodDelete, "/api/v1/exchanges/missing", nil, nil), http.StatusNotFound, "exchange_not_found")

	subscription := bus.Subscribe("")
	defer subscription.Close()
	if len(subscription.Replay) != 1 || subscription.Replay[0].Type != events.ExchangeDeleted || subscription.Replay[0].ExchangeID != completed.ID || subscription.Replay[0].Revision != completed.Revision {
		t.Fatalf("delete replay=%+v", subscription.Replay)
	}
}

func TestConfigurationAPIUpdatesRevisionPersistsAndRejectsStaleWrite(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Mode = config.StorageMemory
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	api, _, bus := testHandler(t, cfg, time.Now)
	body := []byte(`{"expected_revision":1,"overrides":{"capture_response.body":"next","history.max_exchanges":2}}`)
	updated := perform(t, api, http.MethodPut, "/api/v1/config", body, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"revision":2`) || strings.Contains(updated.Body.String(), "secret") {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	assertErrorCode(t, perform(t, api, http.MethodPut, "/api/v1/config", body, nil), http.StatusConflict, "revision_conflict")
	subscription := bus.Subscribe("1")
	defer subscription.Close()
	if subscription.Reset || len(subscription.Replay) != 0 {
		t.Fatalf("unexpected subscription after config event: %+v", subscription)
	}
	removed := perform(t, api, http.MethodDelete, "/api/v1/config/overrides/capture_response.body?expected_revision=2", nil, nil)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"revision":3`) {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
}

func TestConfigurationAPIRemovesAuthenticationOverridesTogether(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Mode = config.StorageMemory
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	api, _, _ := testHandler(t, cfg, time.Now)
	updated := perform(t, api, http.MethodPut, "/api/v1/config", []byte(`{"expected_revision":1,"overrides":{"authentication.username":"operator","authentication.password":"secret"}}`), nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"revision":2`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	removed := perform(t, api, http.MethodPut, "/api/v1/config", []byte(`{"expected_revision":2,"overrides":{},"remove_overrides":["authentication.username","authentication.password"]}`), nil)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"revision":3`) || strings.Contains(removed.Body.String(), "operator") || strings.Contains(removed.Body.String(), "secret") {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
}

func TestAuthenticationProtectsAPIHonorsOriginLogoutAndExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	cfg := config.Defaults()
	cfg.Storage.Mode = config.StorageMemory
	cfg.Auth = config.Authentication{Username: "admin", Password: "secret", SessionTTL: time.Minute}
	api, _, _ := testHandler(t, cfg, func() time.Time { return now })
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/status", nil, nil), http.StatusUnauthorized, "authentication_required")
	badOrigin := perform(t, api, http.MethodPost, "/api/v1/session", []byte(`{"username":"admin","password":"secret"}`), map[string]string{"Origin": "https://attacker.test"})
	assertErrorCode(t, badOrigin, http.StatusForbidden, "origin_rejected")
	login := perform(t, api, http.MethodPost, "/api/v1/session", []byte(`{"username":"admin","password":"secret"}`), nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	authorized := performWithCookie(t, api, http.MethodGet, "/api/v1/status", cookie)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d", authorized.Code)
	}
	now = now.Add(2 * time.Minute)
	assertErrorCode(t, performWithCookie(t, api, http.MethodGet, "/api/v1/events", cookie), http.StatusUnauthorized, "authentication_required")
}

func TestSSEInitialSnapshotReplayResyncAndHeartbeat(t *testing.T) {
	api, repository, bus := testHandlerWithHeartbeat(t, config.Defaults(), time.Now, 5*time.Millisecond)
	repository.Save(apiExchange("one", time.Now(), true))
	server := httptest.NewServer(api)
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	initial := readUntil(t, reader, "\n\n")
	if !strings.Contains(initial, "event: snapshot") || !strings.Contains(initial, `"id":"one"`) {
		t.Fatalf("initial SSE = %q", initial)
	}
	bus.Publish(events.Envelope{Type: events.RequestCompleted, ExchangeID: "one", Revision: 2})
	live := readUntil(t, reader, "\n\n")
	if !strings.Contains(live, "event: request.completed") {
		t.Fatalf("live SSE = %q", live)
	}
	heartbeat := readUntil(t, reader, "\n\n")
	if heartbeat != ": heartbeat\n\n" {
		t.Fatalf("heartbeat = %q", heartbeat)
	}
	_ = response.Body.Close()

	old := bus.Publish(events.Envelope{Type: events.ExchangeStarted, ExchangeID: "old"})
	for index := 0; index < 1100; index++ {
		bus.Publish(events.Envelope{Type: events.RequestCompleted, ExchangeID: "new"})
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	request.Header.Set("Last-Event-ID", old.ID)
	requestContext, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(requestContext)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "event: resync") {
		t.Fatalf("gap response = %q", recorder.Body.String())
	}
}

func TestSSEStopsWhenRuntimeBeginsShutdown(t *testing.T) {
	shutdown := make(chan struct{})
	api, _, _ := testHandlerWithShutdown(t, config.Defaults(), time.Now, shutdown)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.ServeHTTP(recorder, request)
		close(done)
	}()
	close(shutdown)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE remained open after runtime shutdown")
	}
}

func TestVersionedRoutesReturnStructuredResponses(t *testing.T) {
	api, _, _ := testHandler(t, config.Defaults(), time.Now)
	tests := []struct {
		method string
		path   string
		status int
		text   string
	}{
		{http.MethodGet, "/api/v1/health", http.StatusOK, `"status":"ok"`},
		{http.MethodGet, "/api/v1/session", http.StatusOK, `"login_required":false`},
		{http.MethodGet, "/api/v1/config", http.StatusOK, `"revision":1`},
		{http.MethodGet, "/api/v1/storage/sqlite", http.StatusConflict, `"code":"sqlite_disabled"`},
		{http.MethodGet, "/api/v1/missing", http.StatusNotFound, `"code":"route_not_found"`},
		{http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, `"code":"method_not_allowed"`},
	}
	for _, test := range tests {
		response := perform(t, api, test.method, test.path, nil, nil)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.text) {
			t.Errorf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}

func TestStatusIncludesTrafficMetrics(t *testing.T) {
	cfg := config.Defaults()
	manager, err := config.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	api := NewManagementHandler(Options{
		Config: manager, Repository: store.NewMemory(10), Events: events.NewBus(events.Limits{}, time.Now),
		Build: "v1.2.3", Commit: "abc123", BuildDate: "2026-09-08T12:00:00Z", Started: time.Unix(100, 0), Static: http.NotFoundHandler(), Now: func() time.Time { return time.Unix(110, 0) },
		Metrics: func() capture.Metrics {
			return capture.Metrics{CurrentRequests: 2, AdmissionRejected: 3, BodyTooLarge: 4}
		},
		UDPStatus: func() UDPStatus { return UDPStatus{Enabled: true, Listener: "127.0.0.1:9000", Sessions: 2} },
	})
	response := perform(t, api, http.MethodGet, "/api/v1/status", nil, nil)
	for _, expected := range []string{`"build":"v1.2.3"`, `"commit":"abc123"`, `"build_date":"2026-09-08T12:00:00Z"`, `"current_requests":2`, `"admission_rejected":3`, `"body_too_large":4`, `"metrics_started_at":"1970-01-01T00:01:40Z"`, `"udp":{"enabled":true,"listener":"127.0.0.1:9000","sessions":2}`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("status missing %s: %s", expected, response.Body.String())
		}
	}
}

func TestUDPDatagramBodyRouteRejectsHTTPBodyRoutes(t *testing.T) {
	api, repository, _ := testHandler(t, config.Defaults(), time.Now)
	udp := capture.Exchange{ID: "udp", Transport: capture.TransportUDP, Mode: config.ModeCapture, State: capture.StateCompleted, StartedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0), Datagram: &capture.Datagram{SourceAddress: "127.0.0.1:10001", LocalAddress: "127.0.0.1:9000", AcceptedBytes: 6, Payload: []byte("abcdef"), PayloadComplete: true}}
	repository.Save(udp)
	repository.Save(apiExchange("http", time.Unix(3, 0), true))

	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/udp/request/body", nil, nil), http.StatusBadRequest, "transport_body_invalid")
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/udp/response/body", nil, nil), http.StatusBadRequest, "transport_body_invalid")
	body := perform(t, api, http.MethodGet, "/api/v1/exchanges/udp/datagram/body", nil, map[string]string{"Range": "bytes=2-4"})
	if body.Code != http.StatusPartialContent || body.Body.String() != "cde" || !strings.Contains(body.Header().Get("Content-Disposition"), "udp-datagram.bin") {
		t.Fatalf("datagram body status=%d headers=%v body=%q", body.Code, body.Header(), body.Body.String())
	}
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/udp/datagram/body", nil, map[string]string{"Range": "bytes=10-11"}), http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable")
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/http/datagram/body", nil, nil), http.StatusBadRequest, "transport_body_invalid")
}

func TestSSESnapshotAndReplayIncludeUDP(t *testing.T) {
	api, repository, bus := testHandler(t, config.Defaults(), time.Now)
	repository.Save(capture.Exchange{ID: "udp", Transport: capture.TransportUDP, Mode: config.ModeCapture, State: capture.StateCompleted, StartedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0), Datagram: &capture.Datagram{SourceAddress: "127.0.0.1:10001", LocalAddress: "127.0.0.1:9000", AcceptedBytes: 3, PayloadComplete: true}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	requestContext, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(requestContext)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), `"transport":"udp"`) || !strings.Contains(recorder.Body.String(), `"datagram_bytes":3`) {
		t.Fatalf("UDP snapshot=%q", recorder.Body.String())
	}
	anchor := bus.Publish(events.Envelope{Type: events.ExchangeStarted, ExchangeID: "other", Revision: 1})
	event := bus.Publish(events.Envelope{Type: events.RequestCompleted, ExchangeID: "udp", Revision: 1, Data: map[string]any{"transport": "udp", "datagram_bytes": 3}})
	replay := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	replay.Header.Set("Last-Event-ID", anchor.ID)
	replayContext, stopReplay := context.WithCancel(replay.Context())
	stopReplay()
	replay = replay.WithContext(replayContext)
	replayRecorder := httptest.NewRecorder()
	api.ServeHTTP(replayRecorder, replay)
	if !strings.Contains(replayRecorder.Body.String(), "id: "+event.ID) || !strings.Contains(replayRecorder.Body.String(), `"transport":"udp"`) {
		t.Fatalf("UDP replay=%q", replayRecorder.Body.String())
	}
}

func TestConfigurationAPIRejectsInvalidRequestsAndEvictsAfterResize(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Mode = config.StorageMemory
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	api, repository, _ := testHandler(t, cfg, time.Now)
	repository.Save(apiExchange("one", time.Unix(1, 0), true))
	repository.Save(apiExchange("two", time.Unix(2, 0), true))
	tests := []struct {
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{http.MethodPut, "/api/v1/config", `{`, http.StatusBadRequest, "invalid_json"},
		{http.MethodPut, "/api/v1/config", `{"expected_revision":1}`, http.StatusBadRequest, "overrides_required"},
		{http.MethodPut, "/api/v1/config", `{"expected_revision":1,"overrides":{"history.max_exchanges":0}}`, http.StatusUnprocessableEntity, "configuration_invalid"},
		{http.MethodDelete, "/api/v1/config/overrides/mode", ``, http.StatusBadRequest, "expected_revision_required"},
		{http.MethodDelete, "/api/v1/config/overrides/mode?expected_revision=1", ``, http.StatusNotFound, "override_not_found"},
	}
	for _, test := range tests {
		assertErrorCode(t, perform(t, api, test.method, test.path, []byte(test.body), nil), test.status, test.code)
	}
	resize := perform(t, api, http.MethodPut, "/api/v1/config", []byte(`{"expected_revision":1,"overrides":{"history.max_exchanges":1}}`), nil)
	if resize.Code != http.StatusOK || len(repository.List()) != 1 || repository.List()[0].ID != "two" {
		t.Fatalf("resize status=%d exchanges=%v", resize.Code, repository.List())
	}
}

func TestAuthenticationRejectsCredentialsRateLimitsAndLogsOut(t *testing.T) {
	cfg := config.Defaults()
	cfg.Auth = config.Authentication{Username: "admin", Password: "secret", SessionTTL: time.Hour}
	api, repository, _ := testHandler(t, cfg, time.Now)
	repository.Save(capture.Exchange{ID: "udp", Transport: capture.TransportUDP, Mode: config.ModeCapture, State: capture.StateCompleted, StartedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0), Datagram: &capture.Datagram{SourceAddress: "127.0.0.1:10001", LocalAddress: "127.0.0.1:9000", PayloadComplete: true}})
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/udp/datagram/body", nil, nil), http.StatusUnauthorized, "authentication_required")
	assertErrorCode(t, perform(t, api, http.MethodPost, "/api/v1/session", []byte(`{`), nil), http.StatusBadRequest, "invalid_json")
	for attempt := 0; attempt < 5; attempt++ {
		assertErrorCode(t, perform(t, api, http.MethodPost, "/api/v1/session", []byte(`{"username":"admin","password":"wrong"}`), nil), http.StatusUnauthorized, "invalid_credentials")
	}
	assertErrorCode(t, perform(t, api, http.MethodPost, "/api/v1/session", []byte(`{"username":"admin","password":"secret"}`), nil), http.StatusTooManyRequests, "login_rate_limited")

	api, _, _ = testHandler(t, cfg, time.Now)
	login := perform(t, api, http.MethodPost, "/api/v1/session", []byte(`{"username":"admin","password":"secret"}`), nil)
	cookie := login.Result().Cookies()[0]
	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	logoutRequest.AddCookie(cookie)
	logout := httptest.NewRecorder()
	api.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout status=%d cookies=%v", logout.Code, logout.Result().Cookies())
	}
	assertErrorCode(t, performWithCookie(t, api, http.MethodGet, "/api/v1/status", cookie), http.StatusUnauthorized, "authentication_required")
}

func TestResponseBodyAvailabilityAndRangeVariants(t *testing.T) {
	api, repository, _ := testHandler(t, config.Defaults(), time.Now)
	proxy := apiExchange("proxy", time.Now(), true)
	proxy.Mode = config.ModeProxy
	proxy.Response = &capture.Response{Body: []byte("response"), BodyComplete: false}
	repository.Save(proxy)
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/proxy/response/body", nil, nil), http.StatusConflict, "body_not_ready")
	proxy.Response.BodyComplete = true
	repository.Save(proxy)
	suffix := perform(t, api, http.MethodGet, "/api/v1/exchanges/proxy/response/body", nil, map[string]string{"Range": "bytes=-3"})
	if suffix.Code != http.StatusPartialContent || suffix.Body.String() != "nse" {
		t.Fatalf("suffix response=%d/%q", suffix.Code, suffix.Body.String())
	}
	empty := apiExchange("empty", time.Now(), true)
	empty.Request.Body = nil
	repository.Save(empty)
	if response := perform(t, api, http.MethodGet, "/api/v1/exchanges/empty/request/body", nil, nil); response.Code != http.StatusOK || response.Header().Get("Content-Length") != "0" {
		t.Fatalf("empty body response=%d headers=%v", response.Code, response.Header())
	}
	assertErrorCode(t, perform(t, api, http.MethodGet, "/api/v1/exchanges/missing/request/body", nil, nil), http.StatusNotFound, "exchange_not_found")
}

func TestSSEReplaysFromKnownEvent(t *testing.T) {
	api, _, bus := testHandler(t, config.Defaults(), time.Now)
	first := bus.Publish(events.Envelope{Type: events.ExchangeStarted, ExchangeID: "one"})
	second := bus.Publish(events.Envelope{Type: events.RequestCompleted, ExchangeID: "one"})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	request.Header.Set("Last-Event-ID", first.ID)
	requestContext, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(requestContext)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "event: request.completed") || !strings.Contains(recorder.Body.String(), "id: "+second.ID) || strings.Contains(recorder.Body.String(), "event: snapshot") {
		t.Fatalf("replay response = %q", recorder.Body.String())
	}
}

func testHandler(t *testing.T, cfg config.Config, now func() time.Time) (http.Handler, *store.Memory, *events.Bus) {
	return testHandlerWithHeartbeat(t, cfg, now, time.Second)
}

func testHandlerWithHeartbeat(t *testing.T, cfg config.Config, now func() time.Time, heartbeat time.Duration) (http.Handler, *store.Memory, *events.Bus) {
	return testHandlerWithOptions(t, cfg, now, nil, heartbeat)
}

func testHandlerWithShutdown(t *testing.T, cfg config.Config, now func() time.Time, shutdown <-chan struct{}) (http.Handler, *store.Memory, *events.Bus) {
	return testHandlerWithOptions(t, cfg, now, shutdown, time.Second)
}

func testHandlerWithOptions(t *testing.T, cfg config.Config, now func() time.Time, shutdown <-chan struct{}, heartbeat time.Duration) (http.Handler, *store.Memory, *events.Bus) {
	t.Helper()
	manager, err := config.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := store.NewMemory(cfg.History.MaxExchanges)
	bus := events.NewBus(events.Limits{ReplayCount: 1000, ReplayBytes: 8 << 20, SubscriberCount: 16, SubscriberBytes: 1 << 20}, now)
	api := NewManagementHandler(Options{Config: manager, Repository: repository, Events: bus, Build: "test", Started: now(), Static: http.NotFoundHandler(), Now: now, Heartbeat: heartbeat, Shutdown: shutdown})
	return api, repository, bus
}

func apiExchange(id string, completed time.Time, bodyComplete bool) capture.Exchange {
	return capture.Exchange{ID: id, Mode: config.ModeCapture, State: capture.StateCompleted, Revision: 3, ConfigRevision: 1, StartedAt: completed.Add(-time.Second), CompletedAt: completed, Request: capture.Request{Method: http.MethodPost, Path: "/test", Body: []byte("abcdef"), BodyComplete: bodyComplete, Preview: []byte("abc")}, Configuration: capture.ConfigSnapshot{Capture: config.CaptureResponse{Body: "configured capture response"}}}
}

func perform(t *testing.T, handler http.Handler, method, target string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func performWithCookie(t *testing.T, handler http.Handler, method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeRecorder(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status || !strings.Contains(recorder.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("error status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func readUntil(t *testing.T, reader *bufio.Reader, delimiter string) string {
	t.Helper()
	var result strings.Builder
	for !strings.HasSuffix(result.String(), delimiter) {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		result.WriteString(line)
		if err == io.EOF {
			break
		}
	}
	return result.String()
}
