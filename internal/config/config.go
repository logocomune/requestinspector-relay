package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ardanlabs/conf/v3"
	confyaml "github.com/ardanlabs/conf/v3/yaml"
	"gopkg.in/yaml.v3"
)

const (
	Prefix      = "REQRELAY"
	ModeCapture = "capture"
	ModeProxy   = "proxy"

	StorageMemory = "memory"
	StorageSQLite = "sqlite"
)

type Config struct {
	conf.Version `yaml:"-" json:"-"`
	ConfigPath   string          `conf:"flag:config,env:CONFIG,help:path to YAML configuration" yaml:"-" json:"-"`
	Listeners    Listeners       `yaml:"listeners" json:"listeners"`
	Mode         string          `conf:"help:global operating mode: capture|proxy" yaml:"mode" json:"mode"`
	Upstream     Upstream        `yaml:"upstream" json:"upstream"`
	Capture      CaptureResponse `yaml:"capture_response" json:"capture_response"`
	CORS         CORS            `yaml:"cors" json:"cors"`
	Limits       Limits          `yaml:"limits" json:"limits"`
	Preview      Preview         `yaml:"preview" json:"preview"`
	History      History         `yaml:"history" json:"history"`
	Storage      Storage         `yaml:"storage" json:"storage"`
	Auth         Authentication  `yaml:"authentication" json:"authentication"`
	UDP          UDP             `yaml:"udp" json:"udp"`
	Shutdown     time.Duration   `conf:"flag:shutdown-timeout,env:SHUTDOWN_TIMEOUT,help:graceful shutdown timeout" yaml:"shutdown_timeout" json:"shutdown_timeout"`
}

type Listeners struct {
	Management string `conf:"flag:management-listen,env:MANAGEMENT_LISTEN,help:management listen address" yaml:"management" json:"management"`
	Traffic    string `conf:"flag:traffic-listen,env:TRAFFIC_LISTEN,help:inspected traffic listen address" yaml:"traffic" json:"traffic"`
}

type Upstream struct {
	URL     string        `conf:"flag:upstream-url,env:UPSTREAM_URL,help:proxy upstream URL" yaml:"url" json:"url"`
	Timeout time.Duration `conf:"flag:upstream-timeout,env:UPSTREAM_TIMEOUT,help:proxy exchange timeout" yaml:"timeout" json:"timeout"`
}

type CaptureResponse struct {
	Status  int                 `conf:"flag:capture-status,env:CAPTURE_STATUS,help:capture response status" yaml:"status" json:"status"`
	Headers map[string][]string `conf:"-" yaml:"headers" json:"headers"`
	Body    string              `conf:"flag:capture-body,env:CAPTURE_BODY,help:capture response body" yaml:"body" json:"body"`
}

type CORS struct {
	Enabled bool `conf:"flag:cors-enabled,env:CORS_ENABLED,help:enable CORS on HTTP traffic ingest" yaml:"enabled" json:"enabled"`
}

type Limits struct {
	RequestBodyBytes    int64 `conf:"flag:max-request-body-bytes,env:MAX_REQUEST_BODY_BYTES,help:maximum accepted request body bytes" yaml:"request_body_bytes" json:"request_body_bytes"`
	ResponseBodyBytes   int64 `conf:"flag:max-response-body-bytes,env:MAX_RESPONSE_BODY_BYTES,help:maximum accepted upstream response body bytes" yaml:"response_body_bytes" json:"response_body_bytes"`
	RequestHeaderBytes  int   `conf:"flag:max-request-header-bytes,env:MAX_REQUEST_HEADER_BYTES,help:maximum request header bytes" yaml:"request_header_bytes" json:"request_header_bytes"`
	ResponseHeaderBytes int   `conf:"flag:max-response-header-bytes,env:MAX_RESPONSE_HEADER_BYTES,help:maximum upstream response header bytes" yaml:"response_header_bytes" json:"response_header_bytes"`
	ConcurrentExchanges int   `conf:"flag:max-concurrent-exchanges,env:MAX_CONCURRENT_EXCHANGES,help:maximum concurrent exchanges" yaml:"concurrent_exchanges" json:"concurrent_exchanges"`
}

type Preview struct {
	Enabled bool  `conf:"flag:preview-enabled,env:PREVIEW_ENABLED,help:truncate exchange previews" yaml:"enabled" json:"enabled"`
	Bytes   int64 `conf:"flag:preview-bytes,env:PREVIEW_BYTES,help:maximum preview bytes" yaml:"bytes" json:"bytes"`
}

type History struct {
	MaxExchanges int `conf:"flag:history-max-exchanges,env:HISTORY_MAX_EXCHANGES,help:maximum completed exchanges retained in RAM" yaml:"max_exchanges" json:"max_exchanges"`
}

type Storage struct {
	Mode          string `conf:"flag:storage-mode,env:STORAGE_MODE,help:persistence mode: memory|sqlite" yaml:"mode" json:"mode"`
	SQLitePath    string `conf:"flag:sqlite-path,env:SQLITE_PATH,help:SQLite database path; empty selects platform default" yaml:"sqlite_path" json:"sqlite_path"`
	RetentionDays int    `conf:"flag:retention-days,env:RETENTION_DAYS,help:history retention in days; zero disables age cleanup" yaml:"retention_days" json:"retention_days"`
}

type Authentication struct {
	Username   string        `conf:"flag:auth-username,env:AUTH_USERNAME,help:optional management username" yaml:"username" json:"-"`
	Password   string        `conf:"flag:auth-password,env:AUTH_PASSWORD,mask,help:optional management password" yaml:"password" json:"-"`
	SessionTTL time.Duration `conf:"flag:auth-session-ttl,env:AUTH_SESSION_TTL,help:management session lifetime" yaml:"session_ttl" json:"session_ttl"`
}

type UDP struct {
	Enabled                bool          `conf:"flag:udp-enabled,env:UDP_ENABLED,help:enable UDP listener" yaml:"enabled" json:"enabled"`
	Listen                 string        `conf:"flag:udp-listen,env:UDP_LISTEN,help:UDP listen address" yaml:"listen" json:"listen"`
	Mode                   string        `conf:"flag:udp-mode,env:UDP_MODE,help:UDP operating mode: capture|proxy" yaml:"mode" json:"mode"`
	Upstream               string        `conf:"flag:udp-upstream,env:UDP_UPSTREAM,help:UDP proxy upstream address" yaml:"upstream" json:"upstream"`
	MaxDatagramBytes       int           `conf:"flag:udp-max-datagram-bytes,env:UDP_MAX_DATAGRAM_BYTES,help:maximum UDP datagram bytes" yaml:"max_datagram_bytes" json:"max_datagram_bytes"`
	MaxSessions            int           `conf:"flag:udp-max-sessions,env:UDP_MAX_SESSIONS,help:maximum UDP proxy sessions" yaml:"max_sessions" json:"max_sessions"`
	SessionTTL             time.Duration `conf:"flag:udp-session-ttl,env:UDP_SESSION_TTL,help:UDP proxy session lifetime" yaml:"session_ttl" json:"session_ttl"`
	CaptureResponse        string        `conf:"flag:udp-capture-response,env:UDP_CAPTURE_RESPONSE,help:optional UDP capture reply" yaml:"capture_response" json:"capture_response"`
	MaxRepliesPerSecond    int           `conf:"flag:udp-max-replies-per-second,env:UDP_MAX_REPLIES_PER_SECOND,help:maximum UDP upstream replies per session each second" yaml:"max_replies_per_second" json:"max_replies_per_second"`
	MaxReplyBytesPerSecond int64         `conf:"flag:udp-max-reply-bytes-per-second,env:UDP_MAX_REPLY_BYTES_PER_SECOND,help:maximum UDP upstream reply bytes per session each second" yaml:"max_reply_bytes_per_second" json:"max_reply_bytes_per_second"`
}

type persistedDocument struct {
	Base        yaml.Node            `yaml:"base"`
	UIOverrides map[string]yaml.Node `yaml:"ui_overrides"`
}

type loadedDocument struct {
	baseData  []byte
	overrides map[string]yaml.Node
}

func Load(build string) (Config, string, error) {
	path, explicit, err := ResolveConfigPath(os.Args[1:], os.LookupEnv, defaultConfigPath)
	if err != nil {
		return Config{}, "", err
	}

	document, err := readDocument(path, explicit)
	if err != nil {
		return Config{}, "", err
	}

	cfg := Defaults()
	cfg.Version = conf.Version{Build: build, Desc: "RequestInspector Relay HTTP request inspector and reverse proxy"}
	cfg.ConfigPath = path
	options := []conf.ParseOption{conf.WithStrictFlags()}
	if len(document.baseData) > 0 {
		options = append(options, conf.WithParser(confyaml.WithData(document.baseData)))
	}
	info, err := conf.ParseWithOptions(Prefix, &cfg, options...)
	if err != nil {
		return Config{}, info, fmt.Errorf("parse configuration: %w", err)
	}
	if cfg.Capture.Headers == nil {
		cfg.Capture.Headers = map[string][]string{"Content-Type": {"application/json"}}
	}
	if err := applyOverrides(&cfg, document.overrides); err != nil {
		return Config{}, info, fmt.Errorf("apply UI overrides: %w", err)
	}
	if err := resolveSQLitePath(&cfg, path); err != nil {
		return Config{}, info, err
	}
	if err := Validate(cfg); err != nil {
		return Config{}, info, err
	}
	return cfg, info, nil
}

func Defaults() Config {
	return Config{
		Listeners: Listeners{Management: "127.0.0.1:8080", Traffic: "0.0.0.0:8081"},
		Mode:      ModeCapture,
		Upstream:  Upstream{Timeout: 30 * time.Second},
		Capture:   CaptureResponse{Status: http.StatusOK, Body: `{"ok":true}`},
		CORS:      CORS{},
		Limits: Limits{
			RequestBodyBytes:    1 << 20,
			ResponseBodyBytes:   1 << 20,
			RequestHeaderBytes:  64 << 10,
			ResponseHeaderBytes: 64 << 10,
			ConcurrentExchanges: 128,
		},
		Preview: Preview{Enabled: true, Bytes: 16 << 10},
		History: History{MaxExchanges: 100},
		Storage: Storage{Mode: StorageSQLite},
		Auth:    Authentication{SessionTTL: 24 * time.Hour},
		UDP: UDP{
			Enabled:                false,
			Listen:                 "0.0.0.0:12050",
			Mode:                   ModeCapture,
			MaxDatagramBytes:       65507,
			MaxSessions:            1024,
			SessionTTL:             30 * time.Second,
			MaxRepliesPerSecond:    100,
			MaxReplyBytesPerSecond: 1 << 20,
		},
		Shutdown: 10 * time.Second,
	}
}

func ResolveConfigPath(args []string, lookupEnv func(string) (string, bool), defaultPath func() (string, error)) (string, bool, error) {
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--config" {
			if index+1 >= len(args) || args[index+1] == "" {
				return "", true, errors.New("--config requires a non-empty path")
			}
			return args[index+1], true, nil
		}
		if strings.HasPrefix(argument, "--config=") {
			path := strings.TrimPrefix(argument, "--config=")
			if path == "" {
				return "", true, errors.New("--config requires a non-empty path")
			}
			return path, true, nil
		}
	}
	for _, envKey := range []string{Prefix + "_CONFIG", "REQUESTINSPECTOR_RELAY_CONFIG"} {
		if path, ok := lookupEnv(envKey); ok {
			if path == "" {
				return "", true, errors.New(envKey + " requires a non-empty path")
			}
			return path, true, nil
		}
	}
	path, err := defaultPath()
	return path, false, err
}

func DefaultPaths(goos, home, xdgConfig, xdgData, appData, localAppData string) (string, string, error) {
	switch goos {
	case "darwin":
		if home == "" {
			return "", "", errors.New("home directory is unavailable")
		}
		directory := filepath.Join(home, "Library", "Application Support", "RequestInspectorRelay")
		return filepath.Join(directory, "reqrelay.yaml"), filepath.Join(directory, "reqrelay.db"), nil
	case "windows":
		if appData == "" || localAppData == "" {
			return "", "", errors.New("APPDATA and LOCALAPPDATA are required")
		}
		return filepath.Join(appData, "RequestInspectorRelay", "reqrelay.yaml"), filepath.Join(localAppData, "RequestInspectorRelay", "reqrelay.db"), nil
	default:
		if home == "" && (xdgConfig == "" || xdgData == "") {
			return "", "", errors.New("home directory is unavailable")
		}
		configRoot := xdgConfig
		if configRoot == "" {
			configRoot = filepath.Join(home, ".config")
		}
		dataRoot := xdgData
		if dataRoot == "" {
			dataRoot = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(configRoot, "requestinspector-relay", "reqrelay.yaml"), filepath.Join(dataRoot, "requestinspector-relay", "reqrelay.db"), nil
	}
}

func Validate(cfg Config) error {
	var validationErrors []error
	if cfg.Mode != ModeCapture && cfg.Mode != ModeProxy {
		validationErrors = append(validationErrors, fmt.Errorf("mode must be %q or %q", ModeCapture, ModeProxy))
	}
	if err := validateListener("management listener", cfg.Listeners.Management); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if err := validateListener("traffic listener", cfg.Listeners.Traffic); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if cfg.Listeners.Management == cfg.Listeners.Traffic {
		validationErrors = append(validationErrors, errors.New("management and traffic listeners must differ"))
	}
	if cfg.Mode == ModeProxy {
		if err := validateUpstream(cfg.Upstream.URL); err != nil {
			validationErrors = append(validationErrors, err)
		} else if upstreamLoopsToListener(cfg.Upstream.URL, cfg.Listeners) {
			validationErrors = append(validationErrors, errors.New("upstream URL loops to a local listener"))
		}
	}
	if cfg.Upstream.Timeout <= 0 {
		validationErrors = append(validationErrors, errors.New("upstream timeout must be positive"))
	}
	if cfg.Capture.Status < http.StatusOK || cfg.Capture.Status > 599 {
		validationErrors = append(validationErrors, errors.New("capture response status must be between 200 and 599"))
	}
	for name, values := range cfg.Capture.Headers {
		if !validHeaderName(name) {
			validationErrors = append(validationErrors, fmt.Errorf("capture response header %q is invalid", name))
		}
		switch strings.ToLower(name) {
		case "content-length", "transfer-encoding", "trailer", "connection":
			validationErrors = append(validationErrors, fmt.Errorf("capture response header %q controls HTTP framing", name))
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				validationErrors = append(validationErrors, fmt.Errorf("capture response header %q contains a line break", name))
			}
		}
	}
	if cfg.Limits.RequestBodyBytes <= 0 || cfg.Limits.ResponseBodyBytes <= 0 {
		validationErrors = append(validationErrors, errors.New("body limits must be positive"))
	}
	if cfg.Limits.RequestHeaderBytes <= 0 || cfg.Limits.ResponseHeaderBytes <= 0 {
		validationErrors = append(validationErrors, errors.New("header limits must be positive"))
	}
	if cfg.Limits.ConcurrentExchanges <= 0 {
		validationErrors = append(validationErrors, errors.New("concurrent exchange limit must be positive"))
	}
	if cfg.Preview.Bytes <= 0 {
		validationErrors = append(validationErrors, errors.New("preview byte limit must be positive"))
	} else if cfg.Preview.Bytes > cfg.Limits.RequestBodyBytes || cfg.Preview.Bytes > cfg.Limits.ResponseBodyBytes {
		validationErrors = append(validationErrors, errors.New("preview byte limit must not exceed body limits"))
	}
	if cfg.History.MaxExchanges <= 0 {
		validationErrors = append(validationErrors, errors.New("history capacity must be positive"))
	}
	if cfg.Storage.Mode != StorageMemory && cfg.Storage.Mode != StorageSQLite {
		validationErrors = append(validationErrors, fmt.Errorf("storage mode must be %q or %q", StorageMemory, StorageSQLite))
	}
	if cfg.Storage.RetentionDays < 0 {
		validationErrors = append(validationErrors, errors.New("retention days must not be negative"))
	}
	if (cfg.Auth.Username == "") != (cfg.Auth.Password == "") {
		validationErrors = append(validationErrors, errors.New("authentication username and password must both be set or both be empty"))
	}
	if cfg.Auth.SessionTTL <= 0 {
		validationErrors = append(validationErrors, errors.New("authentication session TTL must be positive"))
	}
	validationErrors = append(validationErrors, validateUDP(cfg)...)
	if cfg.Shutdown <= 0 {
		validationErrors = append(validationErrors, errors.New("shutdown timeout must be positive"))
	}
	return errors.Join(validationErrors...)
}

func validateUDP(cfg Config) []error {
	var validationErrors []error
	if cfg.UDP.Mode != ModeCapture && cfg.UDP.Mode != ModeProxy {
		validationErrors = append(validationErrors, fmt.Errorf("UDP mode must be %q or %q", ModeCapture, ModeProxy))
	}
	if err := validateListener("UDP listener", cfg.UDP.Listen); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if cfg.UDP.Listen == cfg.Listeners.Management || cfg.UDP.Listen == cfg.Listeners.Traffic {
		validationErrors = append(validationErrors, errors.New("UDP listener must differ from management and traffic listeners"))
	}
	if cfg.UDP.MaxDatagramBytes <= 0 || cfg.UDP.MaxDatagramBytes > 65507 {
		validationErrors = append(validationErrors, errors.New("UDP maximum datagram bytes must be between 1 and 65507"))
	}
	if cfg.UDP.MaxSessions <= 0 || cfg.UDP.MaxSessions > 4096 {
		validationErrors = append(validationErrors, errors.New("UDP session count must be between 1 and 4096"))
	}
	if cfg.UDP.SessionTTL <= 0 || cfg.UDP.SessionTTL > 30*time.Second {
		validationErrors = append(validationErrors, errors.New("UDP session TTL must be between 1ns and 30s"))
	}
	if cfg.UDP.MaxRepliesPerSecond <= 0 {
		validationErrors = append(validationErrors, errors.New("UDP reply rate must be positive"))
	}
	if cfg.UDP.MaxReplyBytesPerSecond <= 0 {
		validationErrors = append(validationErrors, errors.New("UDP reply byte rate must be positive"))
	}
	if len(cfg.UDP.CaptureResponse) > cfg.UDP.MaxDatagramBytes {
		validationErrors = append(validationErrors, errors.New("UDP capture response must not exceed maximum datagram bytes"))
	}
	switch cfg.UDP.Mode {
	case ModeCapture:
		if cfg.UDP.Upstream != "" {
			validationErrors = append(validationErrors, errors.New("UDP upstream is allowed only in proxy mode"))
		}
	case ModeProxy:
		if cfg.UDP.Upstream == "" {
			validationErrors = append(validationErrors, errors.New("UDP upstream is required in proxy mode"))
		} else if err := validateUDPUpstream(cfg.UDP.Upstream); err != nil {
			validationErrors = append(validationErrors, err)
		}
		if cfg.UDP.CaptureResponse != "" {
			validationErrors = append(validationErrors, errors.New("UDP capture response is allowed only in capture mode"))
		}
	}
	return validationErrors
}

func validateUDPUpstream(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("UDP upstream %q must be host:port", address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if ip.IsUnspecified() || ip.IsMulticast() || isIPv4Broadcast(ip) {
		return errors.New("UDP upstream must be unicast")
	}
	return nil
}

func isIPv4Broadcast(ip net.IP) bool {
	ipv4 := ip.To4()
	return ipv4 != nil && ipv4[0] == 255 && ipv4[1] == 255 && ipv4[2] == 255 && ipv4[3] == 255
}

func defaultConfigPath() (string, error) {
	home, _ := os.UserHomeDir()
	configPath, _, err := DefaultPaths(runtime.GOOS, home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_DATA_HOME"), os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA"))
	return configPath, err
}

func defaultSQLitePath() (string, error) {
	home, _ := os.UserHomeDir()
	_, sqlitePath, err := DefaultPaths(runtime.GOOS, home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_DATA_HOME"), os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA"))
	return sqlitePath, err
}

func readDocument(path string, explicit bool) (loadedDocument, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return loadedDocument{}, nil
	}
	if err != nil {
		return loadedDocument{}, fmt.Errorf("read configuration %q: %w", path, err)
	}
	if len(data) > 1<<20 {
		return loadedDocument{}, errors.New("configuration exceeds 1 MiB")
	}
	return parseDocument(data)
}

func parseDocument(data []byte) (loadedDocument, error) {
	var document persistedDocument
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return loadedDocument{}, fmt.Errorf("parse YAML: %w", err)
	}
	var baseData []byte
	if document.Base.Kind != 0 {
		var err error
		baseData, err = yaml.Marshal(&document.Base)
		if err != nil {
			return loadedDocument{}, fmt.Errorf("encode YAML base: %w", err)
		}
		baseDecoder := yaml.NewDecoder(bytes.NewReader(baseData))
		baseDecoder.KnownFields(true)
		if err := baseDecoder.Decode(&Config{}); err != nil {
			return loadedDocument{}, fmt.Errorf("validate YAML base: %w", err)
		}
	}
	return loadedDocument{baseData: baseData, overrides: document.UIOverrides}, nil
}

func applyOverrides(cfg *Config, overrides map[string]yaml.Node) error {
	for field, value := range overrides {
		var err error
		switch field {
		case "mode":
			err = value.Decode(&cfg.Mode)
		case "listeners.management":
			err = value.Decode(&cfg.Listeners.Management)
		case "listeners.traffic":
			err = value.Decode(&cfg.Listeners.Traffic)
		case "upstream.url":
			err = value.Decode(&cfg.Upstream.URL)
		case "upstream.timeout":
			err = decodeDuration(value, &cfg.Upstream.Timeout)
		case "capture_response.status":
			err = value.Decode(&cfg.Capture.Status)
		case "capture_response.headers":
			var headers map[string][]string
			err = value.Decode(&headers)
			if err == nil {
				cfg.Capture.Headers = headers
			}
		case "capture_response.body":
			err = value.Decode(&cfg.Capture.Body)
		case "cors.enabled":
			err = value.Decode(&cfg.CORS.Enabled)
		case "limits.request_body_bytes":
			err = value.Decode(&cfg.Limits.RequestBodyBytes)
		case "limits.response_body_bytes":
			err = value.Decode(&cfg.Limits.ResponseBodyBytes)
		case "limits.request_header_bytes":
			err = value.Decode(&cfg.Limits.RequestHeaderBytes)
		case "limits.response_header_bytes":
			err = value.Decode(&cfg.Limits.ResponseHeaderBytes)
		case "limits.concurrent_exchanges":
			err = value.Decode(&cfg.Limits.ConcurrentExchanges)
		case "preview.enabled":
			err = value.Decode(&cfg.Preview.Enabled)
		case "preview.bytes":
			err = value.Decode(&cfg.Preview.Bytes)
		case "history.max_exchanges":
			err = value.Decode(&cfg.History.MaxExchanges)
		case "storage.mode":
			err = value.Decode(&cfg.Storage.Mode)
		case "storage.sqlite_path":
			err = value.Decode(&cfg.Storage.SQLitePath)
		case "storage.retention_days":
			err = value.Decode(&cfg.Storage.RetentionDays)
		case "authentication.username":
			err = value.Decode(&cfg.Auth.Username)
		case "authentication.password":
			err = value.Decode(&cfg.Auth.Password)
		case "authentication.session_ttl":
			err = decodeDuration(value, &cfg.Auth.SessionTTL)
		case "udp.enabled":
			err = value.Decode(&cfg.UDP.Enabled)
		case "udp.listen":
			err = value.Decode(&cfg.UDP.Listen)
		case "udp.mode":
			err = value.Decode(&cfg.UDP.Mode)
		case "udp.upstream":
			err = value.Decode(&cfg.UDP.Upstream)
		case "udp.max_datagram_bytes":
			err = value.Decode(&cfg.UDP.MaxDatagramBytes)
		case "udp.max_sessions":
			err = value.Decode(&cfg.UDP.MaxSessions)
		case "udp.session_ttl":
			err = decodeDuration(value, &cfg.UDP.SessionTTL)
		case "udp.capture_response":
			err = value.Decode(&cfg.UDP.CaptureResponse)
		case "udp.max_replies_per_second":
			err = value.Decode(&cfg.UDP.MaxRepliesPerSecond)
		case "udp.max_reply_bytes_per_second":
			err = value.Decode(&cfg.UDP.MaxReplyBytesPerSecond)
		case "shutdown_timeout":
			err = decodeDuration(value, &cfg.Shutdown)
		default:
			return fmt.Errorf("unknown field %q", field)
		}
		if err != nil {
			return fmt.Errorf("field %q: %w", field, err)
		}
	}
	return nil
}

func decodeDuration(node yaml.Node, target *time.Duration) error {
	var text string
	if err := node.Decode(&text); err != nil {
		return err
	}
	duration, err := time.ParseDuration(text)
	if err != nil {
		return err
	}
	*target = duration
	return nil
}

func resolveSQLitePath(cfg *Config, configPath string) error {
	if cfg.Storage.SQLitePath == "" {
		path, err := defaultSQLitePath()
		if err != nil {
			return fmt.Errorf("resolve default SQLite path: %w", err)
		}
		cfg.Storage.SQLitePath = path
		return nil
	}
	if !filepath.IsAbs(cfg.Storage.SQLitePath) {
		cfg.Storage.SQLitePath = filepath.Join(filepath.Dir(configPath), cfg.Storage.SQLitePath)
	}
	return nil
}

func validateListener(name, address string) error {
	if address == "" {
		return fmt.Errorf("%s is required", name)
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("%s %q: %w", name, address, err)
	}
	return nil
}

func validateUpstream(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("upstream URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("upstream URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("upstream URL authority is required")
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.ForceQuery {
		return errors.New("upstream URL must not contain userinfo, fragment, or query")
	}
	return nil
}

func upstreamLoopsToListener(raw string, listeners Listeners) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	upstreamHost := parsed.Hostname()
	upstreamPort := parsed.Port()
	if upstreamPort == "" {
		if strings.EqualFold(parsed.Scheme, "http") {
			upstreamPort = "80"
		} else if parsed.Scheme == "https" {
			upstreamPort = "443"
		}
	}
	for _, address := range []string{listeners.Management, listeners.Traffic} {
		listenerHost, listenerPort, err := net.SplitHostPort(address)
		if err != nil || listenerPort != upstreamPort {
			continue
		}
		if strings.EqualFold(listenerHost, upstreamHost) || localHostsOverlap(listenerHost, upstreamHost) {
			return true
		}
	}
	return false
}

func localHostsOverlap(listenerHost, upstreamHost string) bool {
	listenerIP := net.ParseIP(listenerHost)
	upstreamIP := net.ParseIP(upstreamHost)
	listenerLocal := listenerHost == "" || strings.EqualFold(listenerHost, "localhost") || listenerIP != nil && (listenerIP.IsLoopback() || listenerIP.IsUnspecified())
	upstreamLocal := strings.EqualFold(upstreamHost, "localhost") || upstreamIP != nil && upstreamIP.IsLoopback()
	return listenerLocal && upstreamLocal
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if character > 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={} \t", character) || character < 33 {
			return false
		}
	}
	return true
}
