package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
	"github.com/logocomune/requestinspector-relay/internal/store"
)

type Options struct {
	Config                  *config.Manager
	Repository              *store.Memory
	Persistent              *requestsqlite.Repository
	Events                  *events.Bus
	Build                   string
	Commit                  string
	BuildDate               string
	Started                 time.Time
	Static                  http.Handler
	Now                     func() time.Time
	Heartbeat               time.Duration
	Metrics                 func() capture.Metrics
	UDPStatus               func() UDPStatus
	Shutdown                <-chan struct{}
	ManagementListener      string
	TrafficListener         string
	TrafficListenerAddress  func() string
	PrepareManagementReload func(config.Config) (ManagementReloadPlan, error)
	PrepareTrafficReload    func(config.Config) (TrafficReloadPlan, error)
	PrepareUDPReload        func(config.Config) (UDPReloadPlan, error)
}

type ManagementReloadPlan interface {
	Result() ManagementReload
	Activate()
	Abort()
}

type ManagementReload struct {
	Listener        string `json:"listener"`
	AddressChanged  bool   `json:"address_changed"`
	ReloginRequired bool   `json:"relogin_required"`
}

type TrafficReloadPlan interface {
	Result() TrafficReload
	Activate()
	Abort()
}

type TrafficReload struct {
	Listener       string `json:"listener"`
	AddressChanged bool   `json:"address_changed"`
}

type UDPReloadPlan interface {
	Result() UDPReload
	Activate()
	Abort()
}

type UDPReload struct {
	Enabled        bool   `json:"enabled"`
	Listener       string `json:"listener"`
	AddressChanged bool   `json:"address_changed"`
}

type UDPStatus struct {
	Enabled  bool   `json:"enabled"`
	Listener string `json:"listener"`
	Sessions int    `json:"sessions"`
}

type handler struct {
	configUpdateMutex       sync.Mutex
	config                  *config.Manager
	repository              *store.Memory
	persistent              *requestsqlite.Repository
	events                  *events.Bus
	build                   string
	commit                  string
	buildDate               string
	started                 time.Time
	now                     func() time.Time
	heartbeat               time.Duration
	metrics                 func() capture.Metrics
	auth                    *authenticator
	managementListener      string
	trafficListener         func() string
	udpStatus               func() UDPStatus
	shutdown                <-chan struct{}
	prepareManagementReload func(config.Config) (ManagementReloadPlan, error)
	prepareTrafficReload    func(config.Config) (TrafficReloadPlan, error)
	prepareUDPReload        func(config.Config) (UDPReloadPlan, error)
}

type statusResponse struct {
	Build              string                `json:"build"`
	Commit             string                `json:"commit"`
	BuildDate          string                `json:"build_date"`
	Mode               string                `json:"mode"`
	ManagementListener string                `json:"management_listener"`
	TrafficListener    string                `json:"traffic_listener"`
	UptimeSeconds      int64                 `json:"uptime_seconds"`
	RAMExchanges       int                   `json:"ram_exchanges"`
	RAMCapacity        int                   `json:"ram_capacity"`
	ConfigRevision     uint64                `json:"config_revision"`
	CurrentRequests    int                   `json:"current_requests"`
	AdmissionRejected  uint64                `json:"admission_rejected"`
	BodyTooLarge       uint64                `json:"body_too_large"`
	MetricsStartedAt   time.Time             `json:"metrics_started_at"`
	Storage            *requestsqlite.Status `json:"storage,omitempty"`
	UDP                UDPStatus             `json:"udp"`
}

type configUpdate struct {
	ExpectedRevision uint64                     `json:"expected_revision"`
	Overrides        map[string]json.RawMessage `json:"overrides"`
	RemoveOverrides  []string                   `json:"remove_overrides"`
}

type configurationChange struct {
	expected uint64
	values   map[string]json.RawMessage
	removals []string
}

type configUpdateResponse struct {
	config.ConfigurationView
	ManagementReload *ManagementReload `json:"management_reload,omitempty"`
	TrafficReload    *TrafficReload    `json:"traffic_reload,omitempty"`
	UDPReload        *UDPReload        `json:"udp_reload,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func NewManagementHandler(options Options) http.Handler {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Heartbeat <= 0 {
		options.Heartbeat = 15 * time.Second
	}
	if options.Metrics == nil {
		options.Metrics = func() capture.Metrics { return capture.Metrics{} }
	}
	if options.UDPStatus == nil {
		options.UDPStatus = func() UDPStatus { return UDPStatus{} }
	}
	cfg, _ := options.Config.Current()
	managementListener := options.ManagementListener
	if managementListener == "" {
		managementListener = cfg.Listeners.Management
	}
	trafficListener := options.TrafficListenerAddress
	if trafficListener == nil {
		address := options.TrafficListener
		if address == "" {
			address = cfg.Listeners.Traffic
		}
		trafficListener = func() string { return address }
	}
	api := &handler{config: options.Config, repository: options.Repository, persistent: options.Persistent, events: options.Events, build: options.Build, commit: options.Commit, buildDate: options.BuildDate, started: options.Started, now: options.Now, heartbeat: options.Heartbeat, metrics: options.Metrics, udpStatus: options.UDPStatus, shutdown: options.Shutdown, auth: newAuthenticator(cfg.Auth, options.Now), managementListener: managementListener, trafficListener: trafficListener, prepareManagementReload: options.PrepareManagementReload, prepareTrafficReload: options.PrepareTrafficReload, prepareUDPReload: options.PrepareUDPReload}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", api.health)
	mux.HandleFunc("GET /api/v1/session", api.sessionStatus)
	mux.HandleFunc("POST /api/v1/session", api.login)
	mux.HandleFunc("DELETE /api/v1/session", api.logout)
	mux.HandleFunc("GET /api/v1/status", api.protect(api.status))
	mux.HandleFunc("GET /api/v1/config", api.protect(api.configuration))
	mux.HandleFunc("PUT /api/v1/config", api.protect(api.updateConfiguration))
	mux.HandleFunc("DELETE /api/v1/config/overrides/{field}", api.protect(api.removeOverride))
	mux.HandleFunc("GET /api/v1/exchanges", api.protect(api.exchanges))
	mux.HandleFunc("GET /api/v1/exchanges/{id}", api.protect(api.exchange))
	mux.HandleFunc("DELETE /api/v1/exchanges/{id}", api.protect(api.deleteExchange))
	mux.HandleFunc("GET /api/v1/exchanges/{id}/request/body", api.protect(api.requestBody))
	mux.HandleFunc("GET /api/v1/exchanges/{id}/response/body", api.protect(api.responseBody))
	mux.HandleFunc("GET /api/v1/exchanges/{id}/datagram/body", api.protect(api.datagramBody))
	mux.HandleFunc("DELETE /api/v1/exchanges", api.protect(api.clearExchanges))
	mux.HandleFunc("GET /api/v1/events", api.protect(api.eventStream))
	mux.HandleFunc("GET /api/v1/storage/sqlite", api.protect(api.sqliteStatus))
	mux.HandleFunc("GET /api/v1/storage/sqlite/cleanup-preview", api.protect(api.sqliteCleanupPreview))
	mux.HandleFunc("POST /api/v1/storage/sqlite/cleanup", api.protect(api.sqliteCleanup))
	mux.HandleFunc("POST /api/v1/storage/sqlite/compact", api.protect(api.sqliteCompact))
	mux.HandleFunc("POST /api/v1/storage/sqlite/vacuum", api.protect(api.sqliteVacuum))
	for _, pattern := range []string{
		"/api/v1/health", "/api/v1/session", "/api/v1/status", "/api/v1/config",
		"/api/v1/config/overrides/{field}", "/api/v1/exchanges", "/api/v1/exchanges/{id}",
		"/api/v1/exchanges/{id}/request/body", "/api/v1/exchanges/{id}/response/body", "/api/v1/exchanges/{id}/datagram/body", "/api/v1/events",
		"/api/v1/storage/sqlite", "/api/v1/storage/sqlite/cleanup-preview", "/api/v1/storage/sqlite/cleanup",
		"/api/v1/storage/sqlite/compact", "/api/v1/storage/sqlite/vacuum",
	} {
		mux.HandleFunc(pattern, methodNotAllowed)
	}
	mux.HandleFunc("/api/", func(writer http.ResponseWriter, _ *http.Request) {
		writeError(writer, http.StatusNotFound, "route_not_found", "API route does not exist.")
	})
	mux.Handle("/", options.Static)
	return securityHeaders(mux)
}

func methodNotAllowed(writer http.ResponseWriter, _ *http.Request) {
	writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "HTTP method is not allowed for this route.")
}

func (api *handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *handler) sessionStatus(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]bool{"login_required": api.auth.required(), "authenticated": api.auth.valid(request)})
}

func (api *handler) login(writer http.ResponseWriter, request *http.Request) {
	if !validMutationOrigin(request) {
		writeError(writer, http.StatusForbidden, "origin_rejected", "Request origin is not allowed.")
		return
	}
	var credentials loginRequest
	if err := decodeJSONBody(writer, request, &credentials); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "Login request is invalid.")
		return
	}
	token, expires, code := api.auth.login(request.RemoteAddr, credentials.Username, credentials.Password)
	if code != "" {
		status := http.StatusUnauthorized
		if code == "login_rate_limited" || code == "session_capacity_exhausted" {
			status = http.StatusTooManyRequests
		}
		writeError(writer, status, code, "Login failed.")
		return
	}
	http.SetCookie(writer, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/api/v1", Expires: expires, MaxAge: max(1, int(expires.Sub(api.now()).Seconds())), HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteStrictMode})
	writeJSON(writer, http.StatusOK, map[string]any{"authenticated": true, "expires_at": expires.UTC()})
}

func (api *handler) logout(writer http.ResponseWriter, request *http.Request) {
	if !validMutationOrigin(request) {
		writeError(writer, http.StatusForbidden, "origin_rejected", "Request origin is not allowed.")
		return
	}
	api.auth.logout(request)
	http.SetCookie(writer, &http.Cookie{Name: sessionCookieName, Path: "/api/v1", MaxAge: -1, HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteStrictMode})
	writer.WriteHeader(http.StatusNoContent)
}

func (api *handler) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if !api.auth.valid(request) {
			writeError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead && !validMutationOrigin(request) {
			writeError(writer, http.StatusForbidden, "origin_rejected", "Request origin is not allowed.")
			return
		}
		next(writer, request)
	}
}

func (api *handler) status(writer http.ResponseWriter, _ *http.Request) {
	cfg, revision := api.config.Current()
	metrics := api.metrics()
	response := statusResponse{Build: api.build, Commit: api.commit, BuildDate: api.buildDate, Mode: cfg.Mode, ManagementListener: api.managementListener, TrafficListener: api.trafficListener(), UptimeSeconds: max(0, int64(api.now().Sub(api.started).Seconds())), RAMExchanges: len(api.repository.List()), RAMCapacity: cfg.History.MaxExchanges, ConfigRevision: revision, CurrentRequests: metrics.CurrentRequests, AdmissionRejected: metrics.AdmissionRejected, BodyTooLarge: metrics.BodyTooLarge, MetricsStartedAt: api.started.UTC(), UDP: api.udpStatus()}
	if api.persistent != nil {
		if storage, err := api.persistent.Status(context.Background()); err == nil {
			response.Storage = &storage
		}
	}
	writeJSON(writer, http.StatusOK, response)
}

func (api *handler) configuration(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, api.config.View())
}

func (api *handler) updateConfiguration(writer http.ResponseWriter, request *http.Request) {
	var update configUpdate
	if err := decodeJSONBody(writer, request, &update); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "Configuration update is invalid.")
		return
	}
	if update.Overrides == nil {
		writeError(writer, http.StatusBadRequest, "overrides_required", "Overrides are required.")
		return
	}
	api.configUpdateMutex.Lock()
	defer api.configUpdateMutex.Unlock()
	api.applyConfigurationUpdate(writer, configurationChange{expected: update.ExpectedRevision, values: update.Overrides, removals: update.RemoveOverrides})
}

func (api *handler) removeOverride(writer http.ResponseWriter, request *http.Request) {
	expected, err := strconv.ParseUint(request.URL.Query().Get("expected_revision"), 10, 64)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "expected_revision_required", "Expected revision is required.")
		return
	}
	field := request.PathValue("field")
	api.configUpdateMutex.Lock()
	defer api.configUpdateMutex.Unlock()
	view := api.config.View()
	removals := []string{field}
	if field == "authentication" {
		removals = []string{"authentication.username", "authentication.password"}
		if !containsAny(view.Overrides, removals) {
			writeError(writer, http.StatusNotFound, "override_not_found", "Configuration override does not exist.")
			return
		}
	} else if !containsAny(view.Overrides, removals) {
		writeError(writer, http.StatusNotFound, "override_not_found", "Configuration override does not exist.")
		return
	}
	api.applyConfigurationUpdate(writer, configurationChange{expected: expected, values: map[string]json.RawMessage{}, removals: removals})
}

func (api *handler) applyConfigurationUpdate(writer http.ResponseWriter, change configurationChange) {
	current, _ := api.config.Current()
	prepared, err := api.config.Prepare(config.UpdateRequest{ExpectedRevision: change.expected, Values: change.values, Removals: change.removals})
	if err != nil {
		api.writeConfigError(writer, err)
		return
	}
	next := prepared.Effective()
	var reloadPlan ManagementReloadPlan
	if managementSettingsChanged(current, next) && api.prepareManagementReload != nil {
		reloadPlan, err = api.prepareManagementReload(next)
		if err != nil {
			writeError(writer, http.StatusConflict, "management_listener_unavailable", "Management listener is unavailable; current endpoint remains active.")
			return
		}
		defer func() {
			if reloadPlan != nil {
				reloadPlan.Abort()
			}
		}()
	}
	var trafficReloadPlan TrafficReloadPlan
	if trafficSettingsChanged(current, next) && api.prepareTrafficReload != nil {
		trafficReloadPlan, err = api.prepareTrafficReload(next)
		if err != nil {
			writeError(writer, http.StatusConflict, "traffic_listener_unavailable", "Traffic listener is unavailable; current endpoint remains active.")
			return
		}
		defer func() {
			if trafficReloadPlan != nil {
				trafficReloadPlan.Abort()
			}
		}()
	}
	var udpReloadPlan UDPReloadPlan
	if current.UDP != next.UDP && api.prepareUDPReload != nil {
		udpReloadPlan, err = api.prepareUDPReload(next)
		if err != nil {
			writeError(writer, http.StatusConflict, "udp_listener_unavailable", "UDP listener or upstream is unavailable; current endpoint remains active.")
			return
		}
		defer func() {
			if udpReloadPlan != nil {
				udpReloadPlan.Abort()
			}
		}()
	}
	next, revision, err := prepared.Commit()
	if err != nil {
		api.writeConfigError(writer, err)
		return
	}
	api.resizeHistory(next.History.MaxExchanges)
	api.applyStorageSettings(next)
	api.events.Publish(events.Envelope{Type: events.ConfigChanged, Revision: revision})
	response := configUpdateResponse{ConfigurationView: api.config.View()}
	if reloadPlan != nil {
		result := reloadPlan.Result()
		response.ManagementReload = &result
	}
	if trafficReloadPlan != nil {
		result := trafficReloadPlan.Result()
		response.TrafficReload = &result
	}
	if udpReloadPlan != nil {
		result := udpReloadPlan.Result()
		response.UDPReload = &result
	}
	writeJSON(writer, http.StatusOK, response)
	if udpReloadPlan != nil {
		udpReloadPlan.Activate()
		udpReloadPlan = nil
	}
	if trafficReloadPlan != nil {
		trafficReloadPlan.Activate()
		trafficReloadPlan = nil
	}
	if reloadPlan != nil {
		reloadPlan.Activate()
		reloadPlan = nil
	}
}

func managementSettingsChanged(current, next config.Config) bool {
	return current.Listeners.Management != next.Listeners.Management || current.Auth != next.Auth
}

func trafficSettingsChanged(current, next config.Config) bool {
	return current.Listeners.Traffic != next.Listeners.Traffic || current.CORS != next.CORS
}

func containsAny(values, candidates []string) bool {
	for _, value := range values {
		for _, candidate := range candidates {
			if value == candidate {
				return true
			}
		}
	}
	return false
}

func (api *handler) resizeHistory(capacity int) {
	for _, evicted := range api.repository.Resize(capacity) {
		api.events.Publish(events.Envelope{Type: events.ExchangeEvicted, ExchangeID: evicted.ID, Revision: evicted.Revision})
	}
}

func (api *handler) applyStorageSettings(cfg config.Config) {
	if api.persistent != nil {
		api.persistent.SetRetentionDays(cfg.Storage.RetentionDays)
	}
}

func (api *handler) writeConfigError(writer http.ResponseWriter, err error) {
	if errors.Is(err, config.ErrRevisionConflict) {
		writeError(writer, http.StatusConflict, "revision_conflict", "Configuration revision is stale.")
		return
	}
	writeError(writer, http.StatusUnprocessableEntity, "configuration_invalid", err.Error())
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(request.URL.Path, "/api/") {
			writer.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return
	}
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
