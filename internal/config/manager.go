package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ardanlabs/conf/v3"
	confyaml "github.com/ardanlabs/conf/v3/yaml"
	"gopkg.in/yaml.v3"
)

var ErrRevisionConflict = errors.New("configuration revision conflict")

type Manager struct {
	mutex     sync.RWMutex
	lower     Config
	effective Config
	base      yaml.Node
	overrides map[string]yaml.Node
	origins   map[string]string
	revision  uint64
	path      string
}

type ConfigurationView struct {
	Effective                Config            `json:"effective"`
	ConfigPath               string            `json:"config_path"`
	AuthenticationUsername   string            `json:"authentication_username,omitempty"`
	AuthenticationConfigured bool              `json:"authentication_configured,omitempty"`
	Origins                  map[string]string `json:"origins"`
	Overrides                []string          `json:"overrides"`
	PendingRestart           []string          `json:"pending_restart"`
	Revision                 uint64            `json:"revision"`
}

type PreparedUpdate struct {
	manager   *Manager
	expected  uint64
	overrides map[string]yaml.Node
	effective Config
}

type UpdateRequest struct {
	ExpectedRevision uint64
	Values           map[string]json.RawMessage
	Removals         []string
}

func NewManager(initial Config) (*Manager, error) {
	manager := &Manager{
		lower:     cloneConfig(initial),
		effective: cloneConfig(initial),
		overrides: make(map[string]yaml.Node),
		revision:  1,
		path:      initial.ConfigPath,
		origins:   initialOrigins(initial.ConfigPath != "", loadedDocument{}),
	}
	if initial.ConfigPath == "" {
		return manager, nil
	}
	document, err := readDocument(initial.ConfigPath, false)
	if err != nil {
		return nil, err
	}
	manager.overrides = cloneNodes(document.overrides)
	manager.origins = initialOrigins(true, document)
	if len(document.baseData) > 0 {
		if err := yaml.Unmarshal(document.baseData, &manager.base); err != nil {
			return nil, fmt.Errorf("decode configuration base: %w", err)
		}
		if manager.base.Kind == yaml.DocumentNode && len(manager.base.Content) == 1 {
			manager.base = *manager.base.Content[0]
		}
	}
	if len(manager.overrides) > 0 {
		lower, err := loadLowerConfiguration(initial, document)
		if err != nil {
			return nil, err
		}
		manager.lower = lower
	}
	return manager, nil
}

func (manager *Manager) Current() (Config, uint64) {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	return cloneConfig(manager.effective), manager.revision
}

func (manager *Manager) View() ConfigurationView {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	overrides := sortedKeys(manager.overrides)
	origins := make(map[string]string, len(manager.origins))
	for field, origin := range manager.origins {
		origins[field] = origin
	}
	for _, field := range overrides {
		origins[field] = "ui"
	}
	effective := cloneConfig(manager.effective)
	authenticationUsername := effective.Auth.Username
	authenticationConfigured := authenticationUsername != "" && effective.Auth.Password != ""
	effective.Auth.Username = ""
	effective.Auth.Password = ""
	return ConfigurationView{
		Effective:                effective,
		ConfigPath:               manager.path,
		AuthenticationUsername:   authenticationUsername,
		AuthenticationConfigured: authenticationConfigured,
		Origins:                  origins,
		Overrides:                overrides,
		PendingRestart:           pendingRestart(overrides),
		Revision:                 manager.revision,
	}
}

func (manager *Manager) Update(expected uint64, values map[string]json.RawMessage) (Config, uint64, error) {
	return manager.UpdateWithRemovals(expected, values, nil)
}

func (manager *Manager) UpdateWithRemovals(expected uint64, values map[string]json.RawMessage, removals []string) (Config, uint64, error) {
	prepared, err := manager.Prepare(UpdateRequest{ExpectedRevision: expected, Values: values, Removals: removals})
	if err != nil {
		_, revision := manager.Current()
		return Config{}, revision, err
	}
	return prepared.Commit()
}

func (manager *Manager) Prepare(update UpdateRequest) (*PreparedUpdate, error) {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	if update.ExpectedRevision != manager.revision {
		return nil, ErrRevisionConflict
	}
	nextOverrides := cloneNodes(manager.overrides)
	for _, field := range update.Removals {
		if !knownField(field) {
			return nil, fmt.Errorf("unknown field %q", field)
		}
		delete(nextOverrides, field)
	}
	for field, raw := range update.Values {
		if !knownField(field) {
			return nil, fmt.Errorf("unknown field %q", field)
		}
		node, err := jsonNode(raw)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field, err)
		}
		nextOverrides[field] = node
	}
	next := cloneConfig(manager.lower)
	if err := applyOverrides(&next, nextOverrides); err != nil {
		return nil, err
	}
	if err := Validate(next); err != nil {
		return nil, err
	}
	return &PreparedUpdate{manager: manager, expected: update.ExpectedRevision, overrides: nextOverrides, effective: next}, nil
}

func (prepared *PreparedUpdate) Effective() Config {
	return cloneConfig(prepared.effective)
}

func (prepared *PreparedUpdate) Commit() (Config, uint64, error) {
	manager := prepared.manager
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if prepared.expected != manager.revision {
		return Config{}, manager.revision, ErrRevisionConflict
	}
	if err := manager.persist(prepared.overrides); err != nil {
		return Config{}, manager.revision, err
	}
	manager.effective = cloneConfig(prepared.effective)
	manager.overrides = cloneNodes(prepared.overrides)
	manager.revision++
	return cloneConfig(manager.effective), manager.revision, nil
}

func (manager *Manager) Remove(expected uint64, field string) (Config, uint64, error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if expected != manager.revision {
		return Config{}, manager.revision, ErrRevisionConflict
	}
	if _, ok := manager.overrides[field]; !ok {
		return Config{}, manager.revision, os.ErrNotExist
	}
	nextOverrides := cloneNodes(manager.overrides)
	delete(nextOverrides, field)
	return manager.activate(nextOverrides)
}

func (manager *Manager) RemoveAuthentication(expected uint64) (Config, uint64, error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if expected != manager.revision {
		return Config{}, manager.revision, ErrRevisionConflict
	}
	nextOverrides := cloneNodes(manager.overrides)
	removed := false
	for _, field := range []string{"authentication.username", "authentication.password"} {
		if _, ok := nextOverrides[field]; ok {
			delete(nextOverrides, field)
			removed = true
		}
	}
	if !removed {
		return Config{}, manager.revision, os.ErrNotExist
	}
	return manager.activate(nextOverrides)
}

func (manager *Manager) activate(overrides map[string]yaml.Node) (Config, uint64, error) {
	next := cloneConfig(manager.lower)
	if err := applyOverrides(&next, overrides); err != nil {
		return Config{}, manager.revision, err
	}
	if err := Validate(next); err != nil {
		return Config{}, manager.revision, err
	}
	if err := manager.persist(overrides); err != nil {
		return Config{}, manager.revision, err
	}
	manager.effective = next
	manager.overrides = overrides
	manager.revision++
	return cloneConfig(next), manager.revision, nil
}

func (manager *Manager) persist(overrides map[string]yaml.Node) error {
	if manager.path == "" {
		return errors.New("configuration path is unavailable")
	}
	base := manager.base
	if base.Kind == 0 {
		base = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	document := persistedDocument{Base: base, UIOverrides: overrides}
	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	directory := filepath.Dir(manager.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".reqrelay-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	defer cleanup()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary configuration: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, manager.path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}

func jsonNode(raw json.RawMessage) (yaml.Node, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&value); err != nil {
		return yaml.Node{}, err
	}
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return yaml.Node{}, err
	}
	return node, nil
}

func cloneConfig(cfg Config) Config {
	clone := cfg
	if cfg.Capture.Headers != nil {
		clone.Capture.Headers = make(map[string][]string, len(cfg.Capture.Headers))
		for name, values := range cfg.Capture.Headers {
			clone.Capture.Headers[name] = append([]string(nil), values...)
		}
	}
	return clone
}

func cloneNodes(nodes map[string]yaml.Node) map[string]yaml.Node {
	clone := make(map[string]yaml.Node, len(nodes))
	for field, node := range nodes {
		clone[field] = node
	}
	return clone
}

func sortedKeys(nodes map[string]yaml.Node) []string {
	keys := make([]string, 0, len(nodes))
	for field := range nodes {
		keys = append(keys, field)
	}
	sort.Strings(keys)
	return keys
}

func pendingRestart(overrides []string) []string {
	result := make([]string, 0)
	for _, field := range overrides {
		if field == "storage.mode" || field == "storage.sqlite_path" {
			result = append(result, field)
		}
	}
	return result
}

func knownField(field string) bool {
	for _, candidate := range knownOverrideFields {
		if candidate == field {
			return true
		}
	}
	return false
}

var knownOverrideFields = []string{
	"mode", "listeners.management", "listeners.traffic", "upstream.url", "upstream.timeout",
	"capture_response.status", "capture_response.headers", "capture_response.body",
	"cors.enabled",
	"limits.request_body_bytes", "limits.response_body_bytes", "limits.request_header_bytes",
	"limits.response_header_bytes", "limits.concurrent_exchanges", "preview.enabled", "preview.bytes",
	"history.max_exchanges", "storage.mode", "storage.sqlite_path", "storage.retention_days",
	"authentication.username", "authentication.password", "authentication.session_ttl", "shutdown_timeout",
	"udp.enabled", "udp.listen", "udp.mode", "udp.upstream", "udp.max_datagram_bytes", "udp.max_sessions", "udp.session_ttl", "udp.capture_response", "udp.max_replies_per_second", "udp.max_reply_bytes_per_second",
}

type fieldSource struct {
	field string
	flag  string
	env   string
}

var fieldSources = []fieldSource{
	{"mode", "mode", "MODE"}, {"listeners.management", "management-listen", "MANAGEMENT_LISTEN"}, {"listeners.traffic", "traffic-listen", "TRAFFIC_LISTEN"},
	{"upstream.url", "upstream-url", "UPSTREAM_URL"}, {"upstream.timeout", "upstream-timeout", "UPSTREAM_TIMEOUT"},
	{"capture_response.status", "capture-status", "CAPTURE_STATUS"}, {"capture_response.headers", "", ""}, {"capture_response.body", "capture-body", "CAPTURE_BODY"},
	{"cors.enabled", "cors-enabled", "CORS_ENABLED"},
	{"limits.request_body_bytes", "max-request-body-bytes", "MAX_REQUEST_BODY_BYTES"}, {"limits.response_body_bytes", "max-response-body-bytes", "MAX_RESPONSE_BODY_BYTES"},
	{"limits.request_header_bytes", "max-request-header-bytes", "MAX_REQUEST_HEADER_BYTES"}, {"limits.response_header_bytes", "max-response-header-bytes", "MAX_RESPONSE_HEADER_BYTES"},
	{"limits.concurrent_exchanges", "max-concurrent-exchanges", "MAX_CONCURRENT_EXCHANGES"}, {"preview.enabled", "preview-enabled", "PREVIEW_ENABLED"},
	{"preview.bytes", "preview-bytes", "PREVIEW_BYTES"}, {"history.max_exchanges", "history-max-exchanges", "HISTORY_MAX_EXCHANGES"},
	{"storage.mode", "storage-mode", "STORAGE_MODE"}, {"storage.sqlite_path", "sqlite-path", "SQLITE_PATH"}, {"storage.retention_days", "retention-days", "RETENTION_DAYS"},
	{"authentication.username", "auth-username", "AUTH_USERNAME"}, {"authentication.password", "auth-password", "AUTH_PASSWORD"},
	{"authentication.session_ttl", "auth-session-ttl", "AUTH_SESSION_TTL"}, {"shutdown_timeout", "shutdown-timeout", "SHUTDOWN_TIMEOUT"},
	{"udp.enabled", "udp-enabled", "UDP_ENABLED"}, {"udp.listen", "udp-listen", "UDP_LISTEN"}, {"udp.mode", "udp-mode", "UDP_MODE"}, {"udp.upstream", "udp-upstream", "UDP_UPSTREAM"},
	{"udp.max_datagram_bytes", "udp-max-datagram-bytes", "UDP_MAX_DATAGRAM_BYTES"}, {"udp.max_sessions", "udp-max-sessions", "UDP_MAX_SESSIONS"}, {"udp.session_ttl", "udp-session-ttl", "UDP_SESSION_TTL"},
	{"udp.capture_response", "udp-capture-response", "UDP_CAPTURE_RESPONSE"}, {"udp.max_replies_per_second", "udp-max-replies-per-second", "UDP_MAX_REPLIES_PER_SECOND"}, {"udp.max_reply_bytes_per_second", "udp-max-reply-bytes-per-second", "UDP_MAX_REPLY_BYTES_PER_SECOND"},
}

func initialOrigins(detect bool, document loadedDocument) map[string]string {
	origins := make(map[string]string, len(fieldSources))
	for _, source := range fieldSources {
		origin := "startup"
		if detect {
			origin = "default"
			if baseContains(document.baseData, source.field) {
				origin = "file"
			}
			if source.env != "" {
				if _, ok := os.LookupEnv(Prefix + "_" + source.env); ok {
					origin = "env"
				}
			}
			if source.flag != "" && commandLineHas(source.flag) {
				origin = "cli"
			}
		}
		origins[source.field] = origin
	}
	return origins
}

func commandLineHas(name string) bool {
	prefix := "--" + name
	for _, argument := range os.Args[1:] {
		if argument == prefix || len(argument) > len(prefix) && argument[:len(prefix)+1] == prefix+"=" {
			return true
		}
	}
	return false
}

func baseContains(data []byte, field string) bool {
	if len(data) == 0 {
		return false
	}
	var values map[string]any
	if err := yaml.Unmarshal(data, &values); err != nil {
		return false
	}
	parts := strings.Split(field, ".")
	var current any = values
	for _, part := range parts {
		mapping, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = mapping[part]
		if !ok {
			return false
		}
	}
	return true
}

func loadLowerConfiguration(initial Config, document loadedDocument) (Config, error) {
	cfg := Defaults()
	cfg.Version = initial.Version
	cfg.ConfigPath = initial.ConfigPath
	options := []conf.ParseOption{conf.WithStrictFlags()}
	if len(document.baseData) > 0 {
		options = append(options, conf.WithParser(confyaml.WithData(document.baseData)))
	}
	if _, err := conf.ParseWithOptions(Prefix, &cfg, options...); err != nil {
		return Config{}, fmt.Errorf("parse lower-priority configuration: %w", err)
	}
	if cfg.Capture.Headers == nil {
		cfg.Capture.Headers = map[string][]string{"Content-Type": {"application/json"}}
	}
	if err := resolveSQLitePath(&cfg, initial.ConfigPath); err != nil {
		return Config{}, err
	}
	return cfg, Validate(cfg)
}
