package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"

	confyaml "github.com/ardanlabs/conf/v3/yaml"
	"gopkg.in/yaml.v3"
)

func TestResolveConfigPath(t *testing.T) {
	defaultPath := func() (string, error) { return "/default/reqrelay.yaml", nil }
	tests := []struct {
		name        string
		args        []string
		environment map[string]string
		want        string
		explicit    bool
		wantError   bool
	}{
		{name: "CLI separate", args: []string{"--config", "/cli.yaml"}, environment: map[string]string{Prefix + "_CONFIG": "/env.yaml"}, want: "/cli.yaml", explicit: true},
		{name: "CLI equals", args: []string{"--config=/cli.yaml"}, want: "/cli.yaml", explicit: true},
		{name: "environment prefix", environment: map[string]string{Prefix + "_CONFIG": "/env.yaml"}, want: "/env.yaml", explicit: true},
		{name: "environment fallback relay", environment: map[string]string{"REQUESTINSPECTOR_RELAY_CONFIG": "/env-relay.yaml"}, want: "/env-relay.yaml", explicit: true},
		{name: "default", want: "/default/reqrelay.yaml"},
		{name: "missing CLI value", args: []string{"--config"}, wantError: true, explicit: true},
		{name: "empty environment", environment: map[string]string{Prefix + "_CONFIG": ""}, wantError: true, explicit: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				value, ok := test.environment[key]
				return value, ok
			}
			path, explicit, err := ResolveConfigPath(test.args, lookup, defaultPath)
			if (err != nil) != test.wantError {
				t.Fatalf("ResolveConfigPath() error = %v, wantError %v", err, test.wantError)
			}
			if path != test.want || explicit != test.explicit {
				t.Fatalf("ResolveConfigPath() = (%q, %v), want (%q, %v)", path, explicit, test.want, test.explicit)
			}
		})
	}
}

func TestDefaultPaths(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		home       string
		xdgConfig  string
		xdgData    string
		appData    string
		localData  string
		wantConfig string
		wantSQLite string
	}{
		{name: "Linux defaults", goos: "linux", home: "/home/test", wantConfig: filepath.Join("/home/test", ".config", "requestinspector-relay", "reqrelay.yaml"), wantSQLite: filepath.Join("/home/test", ".local", "share", "requestinspector-relay", "reqrelay.db")},
		{name: "Linux XDG", goos: "linux", home: "/home/test", xdgConfig: "/config", xdgData: "/data", wantConfig: filepath.Join("/config", "requestinspector-relay", "reqrelay.yaml"), wantSQLite: filepath.Join("/data", "requestinspector-relay", "reqrelay.db")},
		{name: "macOS", goos: "darwin", home: "/Users/test", wantConfig: filepath.Join("/Users/test", "Library", "Application Support", "RequestInspectorRelay", "reqrelay.yaml"), wantSQLite: filepath.Join("/Users/test", "Library", "Application Support", "RequestInspectorRelay", "reqrelay.db")},
		{name: "Windows", goos: "windows", appData: `C:\Users\test\AppData\Roaming`, localData: `C:\Users\test\AppData\Local`, wantConfig: filepath.Join(`C:\Users\test\AppData\Roaming`, "RequestInspectorRelay", "reqrelay.yaml"), wantSQLite: filepath.Join(`C:\Users\test\AppData\Local`, "RequestInspectorRelay", "reqrelay.db")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configPath, sqlitePath, err := DefaultPaths(test.goos, test.home, test.xdgConfig, test.xdgData, test.appData, test.localData)
			if err != nil {
				t.Fatal(err)
			}
			if configPath != test.wantConfig || sqlitePath != test.wantSQLite {
				t.Fatalf("DefaultPaths() = (%q, %q), want (%q, %q)", configPath, sqlitePath, test.wantConfig, test.wantSQLite)
			}
		})
	}
}

func TestLoadPrecedence(t *testing.T) {
	configurationPath := filepath.Join(t.TempDir(), "reqrelay.yaml")
	data := []byte(`base:
  mode: capture
  preview:
    enabled: false
    bytes: 1024
  capture_response:
    body: ""
    headers: {}
  storage:
    mode: memory
  udp:
    enabled: false
    max_sessions: 12
ui_overrides:
  history.max_exchanges: 40
  udp.max_sessions: 34
`)
	if err := os.WriteFile(configurationPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	originalArguments := os.Args
	t.Cleanup(func() { os.Args = originalArguments })
	t.Setenv(Prefix+"_HISTORY_MAX_EXCHANGES", "20")
	os.Args = []string{"reqrelay", "--config", configurationPath, "--history-max-exchanges", "30"}

	cfg, _, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.History.MaxExchanges != 40 {
		t.Fatalf("history capacity = %d, want 40", cfg.History.MaxExchanges)
	}
	if cfg.UDP.Enabled || cfg.UDP.MaxSessions != 34 {
		t.Fatalf("UDP precedence = %+v", cfg.UDP)
	}
	if cfg.Mode != ModeCapture || cfg.Storage.Mode != StorageMemory {
		t.Fatalf("unexpected loaded configuration: mode=%q storage=%q", cfg.Mode, cfg.Storage.Mode)
	}
	if cfg.Preview.Enabled || cfg.Capture.Body != "" || len(cfg.Capture.Headers) != 0 {
		t.Fatalf("YAML zero values not preserved: preview=%v body=%q headers=%v", cfg.Preview.Enabled, cfg.Capture.Body, cfg.Capture.Headers)
	}
}

func TestApplyOverridesPreservesZeroValues(t *testing.T) {
	cfg := validConfig()
	overrides := map[string]yaml.Node{
		"preview.enabled":          scalarNode("false"),
		"storage.retention_days":   scalarNode("0"),
		"capture_response.body":    scalarNode(""),
		"capture_response.headers": mappingNode(t, map[string][]string{}),
		"udp.enabled":              scalarNode("false"),
		"udp.capture_response":     scalarNode(""),
	}
	if err := applyOverrides(&cfg, overrides); err != nil {
		t.Fatal(err)
	}
	if cfg.Preview.Enabled || cfg.Storage.RetentionDays != 0 || cfg.Capture.Body != "" || len(cfg.Capture.Headers) != 0 {
		t.Fatalf("explicit zero values not preserved: %+v", cfg)
	}
	if cfg.UDP.Enabled || cfg.UDP.CaptureResponse != "" {
		t.Fatalf("UDP zero values not preserved: %+v", cfg.UDP)
	}
}

func TestUDPDefaults(t *testing.T) {
	udp := Defaults().UDP
	if udp.Enabled || udp.Listen != "0.0.0.0:12050" || udp.Mode != ModeCapture || udp.Upstream != "" || udp.MaxDatagramBytes != 65507 || udp.MaxSessions != 1024 || udp.SessionTTL != 30*time.Second || udp.CaptureResponse != "" || udp.MaxRepliesPerSecond != 100 || udp.MaxReplyBytesPerSecond != 1<<20 {
		t.Fatalf("Defaults().UDP = %+v", udp)
	}
}

func TestDefaultsListeners(t *testing.T) {
	listeners := Defaults().Listeners
	if listeners.Management != "127.0.0.1:8080" || listeners.Traffic != "0.0.0.0:8081" {
		t.Fatalf("Defaults().Listeners = %+v", listeners)
	}
}

func TestDefaultsCORSDisabled(t *testing.T) {
	if Defaults().CORS.Enabled {
		t.Fatal("CORS enabled by default")
	}
}

func TestApplyOverridesAlwaysWinsProperty(t *testing.T) {
	property := func(base, override uint16) bool {
		cfg := validConfig()
		cfg.History.MaxExchanges = int(base) + 1
		err := applyOverrides(&cfg, map[string]yaml.Node{
			"history.max_exchanges": scalarNode(fmt.Sprint(int(override) + 1)),
		})
		return err == nil && cfg.History.MaxExchanges == int(override)+1
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "valid"},
		{name: "mode", mutate: func(cfg *Config) { cfg.Mode = "invalid" }, want: "mode must be"},
		{name: "same listeners", mutate: func(cfg *Config) { cfg.Listeners.Traffic = cfg.Listeners.Management }, want: "listeners must differ"},
		{name: "proxy target required", mutate: func(cfg *Config) { cfg.Mode = ModeProxy; cfg.Upstream.URL = "" }, want: "scheme must be"},
		{name: "proxy target query", mutate: func(cfg *Config) { cfg.Mode = ModeProxy; cfg.Upstream.URL = "https://example.test?x=1" }, want: "must not contain"},
		{name: "proxy uppercase scheme", mutate: func(cfg *Config) { cfg.Mode = ModeProxy; cfg.Upstream.URL = "HTTPS://example.test" }},
		{name: "proxy target empty query", mutate: func(cfg *Config) { cfg.Mode = ModeProxy; cfg.Upstream.URL = "https://example.test?" }, want: "must not contain"},
		{name: "proxy target loop", mutate: func(cfg *Config) { cfg.Mode = ModeProxy; cfg.Upstream.URL = "http://localhost:8081" }, want: "loops to a local listener"},
		{name: "framing header", mutate: func(cfg *Config) { cfg.Capture.Headers = map[string][]string{"Content-Length": {"1"}} }, want: "controls HTTP framing"},
		{name: "header line break", mutate: func(cfg *Config) { cfg.Capture.Headers = map[string][]string{"X-Test": {"one\r\ntwo"}} }, want: "contains a line break"},
		{name: "preview exceeds body", mutate: func(cfg *Config) { cfg.Preview.Bytes = cfg.Limits.RequestBodyBytes + 1 }, want: "must not exceed"},
		{name: "partial credentials", mutate: func(cfg *Config) { cfg.Auth.Username = "user" }, want: "must both be set"},
		{name: "negative retention", mutate: func(cfg *Config) { cfg.Storage.RetentionDays = -1 }, want: "must not be negative"},
		{name: "UDP mode", mutate: func(cfg *Config) { cfg.UDP.Mode = "invalid" }, want: "UDP mode must be"},
		{name: "UDP listener conflict", mutate: func(cfg *Config) { cfg.UDP.Listen = cfg.Listeners.Traffic }, want: "UDP listener must differ"},
		{name: "UDP proxy upstream required", mutate: func(cfg *Config) { cfg.UDP.Mode = ModeProxy }, want: "UDP upstream is required"},
		{name: "UDP capture upstream", mutate: func(cfg *Config) { cfg.UDP.Upstream = "127.0.0.1:9001" }, want: "UDP upstream is allowed only"},
		{name: "UDP proxy capture response", mutate: func(cfg *Config) {
			cfg.UDP.Mode = ModeProxy
			cfg.UDP.Upstream = "127.0.0.1:9001"
			cfg.UDP.CaptureResponse = "reply"
		}, want: "UDP capture response is allowed only"},
		{name: "UDP oversized maximum", mutate: func(cfg *Config) { cfg.UDP.MaxDatagramBytes = 65508 }, want: "UDP maximum datagram bytes"},
		{name: "UDP session cap", mutate: func(cfg *Config) { cfg.UDP.MaxSessions = 4097 }, want: "UDP session count"},
		{name: "UDP TTL cap", mutate: func(cfg *Config) { cfg.UDP.SessionTTL = 31 * time.Second }, want: "UDP session TTL"},
		{name: "UDP reply exceeds datagram", mutate: func(cfg *Config) { cfg.UDP.CaptureResponse = strings.Repeat("x", cfg.UDP.MaxDatagramBytes+1) }, want: "UDP capture response"},
		{name: "UDP multicast upstream", mutate: func(cfg *Config) { cfg.UDP.Mode = ModeProxy; cfg.UDP.Upstream = "224.0.0.1:9001" }, want: "UDP upstream must be unicast"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			if test.mutate != nil {
				test.mutate(&cfg)
			}
			err := Validate(cfg)
			if test.want == "" && err != nil {
				t.Fatal(err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsCaptureFramingHeaders(t *testing.T) {
	for _, name := range []string{"Content-Length", "Transfer-Encoding", "Trailer", "Connection", "content-length"} {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Capture.Headers = map[string][]string{name: {"value"}}
			if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "controls HTTP framing") {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestParseDocumentRejectsUnknownOverride(t *testing.T) {
	document, err := parseDocument([]byte("ui_overrides:\n  unknown: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := validConfig()
	if err := applyOverrides(&cfg, document.overrides); err == nil {
		t.Fatal("applyOverrides() accepted unknown field")
	}
}

func TestParseDocumentRejectsUnknownBaseField(t *testing.T) {
	_, err := parseDocument([]byte("base:\n  unknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("parseDocument() error = %v", err)
	}
}

func FuzzParseDocument(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("base:\n  mode: capture\nui_overrides: {}\n"),
		[]byte("ui_overrides:\n  preview.enabled: false\n"),
		[]byte("{base: {mode: proxy}, ui_overrides: {storage.retention_days: 0}}"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		document, err := parseDocument(data)
		if err != nil {
			return
		}
		cfg := validConfig()
		if len(document.baseData) > 0 {
			if err := confyaml.WithData(document.baseData).Process(Prefix, &cfg); err != nil {
				return
			}
		}
		_ = applyOverrides(&cfg, document.overrides)
	})
}

func FuzzUDPConfigurationValidation(f *testing.F) {
	f.Add("127.0.0.1:9000", "127.0.0.1:9001", uint16(1), uint16(1), uint8(1))
	f.Add("[::1]:9000", "example.test:9001", uint16(65507), uint16(4096), uint8(2))
	f.Fuzz(func(t *testing.T, listen, upstream string, datagramBytes, sessions uint16, mode uint8) {
		cfg := Defaults()
		cfg.UDP.Enabled = true
		cfg.UDP.Listen = listen
		cfg.UDP.Upstream = upstream
		cfg.UDP.MaxDatagramBytes = int(datagramBytes)
		cfg.UDP.MaxSessions = int(sessions)
		if mode%2 == 0 {
			cfg.UDP.Mode = ModeCapture
			cfg.UDP.Upstream = ""
		} else {
			cfg.UDP.Mode = ModeProxy
		}
		_ = Validate(cfg)
	})
}

func validConfig() Config {
	return Config{
		Listeners: Listeners{Management: "127.0.0.1:8080", Traffic: "0.0.0.0:8081"},
		Mode:      ModeCapture,
		Upstream:  Upstream{Timeout: 30 * time.Second},
		Capture:   CaptureResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: "{}"},
		Limits: Limits{
			RequestBodyBytes:    1 << 20,
			ResponseBodyBytes:   1 << 20,
			RequestHeaderBytes:  64 << 10,
			ResponseHeaderBytes: 64 << 10,
			ConcurrentExchanges: 128,
		},
		Preview:  Preview{Enabled: true, Bytes: 16 << 10},
		History:  History{MaxExchanges: 100},
		Storage:  Storage{Mode: StorageSQLite},
		Auth:     Authentication{SessionTTL: 24 * time.Hour},
		UDP:      Defaults().UDP,
		Shutdown: time.Second,
	}
}

func scalarNode(value string) yaml.Node {
	tag := ""
	if value == "" {
		tag = "!!str"
	}
	return yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

func mappingNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		t.Fatal(err)
	}
	if len(node.Content) != 1 {
		t.Fatalf("unexpected YAML node: %+v", node)
	}
	return *node.Content[0]
}

func TestLoadedConfigurationDoesNotAliasOverrideMaps(t *testing.T) {
	cfg := validConfig()
	headers := map[string][]string{"X-Test": {"one"}}
	if err := applyOverrides(&cfg, map[string]yaml.Node{"capture_response.headers": mappingNode(t, headers)}); err != nil {
		t.Fatal(err)
	}
	headers["X-Test"][0] = "changed"
	if reflect.DeepEqual(cfg.Capture.Headers, headers) {
		t.Fatal("configuration aliases caller map")
	}
}
