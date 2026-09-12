package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerUpdatesPersistsConflictsAndRemovesOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg := validConfig()
	cfg.ConfigPath = path
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	next, revision, err := manager.Update(1, map[string]json.RawMessage{
		"capture_response.body": json.RawMessage(`"changed"`),
		"preview.enabled":       json.RawMessage(`false`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 2 || next.Capture.Body != "changed" || next.Preview.Enabled {
		t.Fatalf("update = revision %d config %+v", revision, next)
	}
	if _, _, err := manager.Update(1, map[string]json.RawMessage{"mode": json.RawMessage(`"capture"`)}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "capture_response.body: changed") || strings.Contains(string(data), "password:") {
		t.Fatalf("persisted document = %s", data)
	}
	next, revision, err = manager.Remove(2, "capture_response.body")
	if err != nil {
		t.Fatal(err)
	}
	if revision != 3 || next.Capture.Body != cfg.Capture.Body {
		t.Fatalf("remove = revision %d body %q", revision, next.Capture.Body)
	}
}

func TestManagerRejectsInvalidOrUnknownOverridesWithoutMutation(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tests := []map[string]json.RawMessage{
		{"unknown": json.RawMessage(`true`)},
		{"history.max_exchanges": json.RawMessage(`0`)},
		{"preview.enabled": json.RawMessage(`{`)},
	}
	for _, values := range tests {
		if _, _, err := manager.Update(1, values); err == nil {
			t.Fatalf("Update(%s) succeeded", values)
		}
		_, revision := manager.Current()
		if revision != 1 {
			t.Fatalf("failed update changed revision to %d", revision)
		}
	}
}

func TestManagerViewShowsUsernameAndOmitsPassword(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg.Auth.Username = "admin"
	cfg.Auth.Password = "secret"
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manager.View())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"config_path":"`+cfg.ConfigPath+`"`) || !strings.Contains(string(data), `"authentication_username":"admin"`) || strings.Contains(string(data), "secret") {
		t.Fatalf("authentication metadata unsafe: %s", data)
	}
}

func TestManagerAppliesUDPPersistentOverridesWithoutRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg := validConfig()
	cfg.ConfigPath = path
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	next, revision, err := manager.Update(1, map[string]json.RawMessage{
		"udp.enabled":      json.RawMessage(`true`),
		"udp.max_sessions": json.RawMessage(`2048`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 2 || !next.UDP.Enabled || next.UDP.MaxSessions != 2048 {
		t.Fatalf("update = revision %d UDP %+v", revision, next.UDP)
	}
	for _, field := range knownOverrideFields {
		if strings.HasPrefix(field, "udp.") && contains(pendingRestart([]string{field}), field) {
			t.Fatalf("UDP field %q is restart-required", field)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "udp.enabled: true") || !strings.Contains(string(data), "udp.max_sessions: 2048") {
		t.Fatalf("persisted document = %s", data)
	}
}

func TestTrafficListenerOverrideDoesNotRequireRestart(t *testing.T) {
	if contains(pendingRestart([]string{"listeners.traffic"}), "listeners.traffic") {
		t.Fatal("traffic listener is restart-required")
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestManagerReloadsLowerPriorityValueBeforeRemovingPersistedOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reqrelay.yaml")
	data := []byte("base:\n  history:\n    max_exchanges: 12\n  storage:\n    mode: memory\nui_overrides:\n  history.max_exchanges: 34\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	originalArguments := os.Args
	os.Args = []string{"reqrelay", "--config", path}
	t.Cleanup(func() { os.Args = originalArguments })
	cfg, _, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	view := manager.View()
	if view.Origins["history.max_exchanges"] != "ui" || view.Origins["storage.mode"] != "file" {
		t.Fatalf("origins = %v", view.Origins)
	}
	next, revision, err := manager.Remove(1, "history.max_exchanges")
	if err != nil {
		t.Fatal(err)
	}
	if revision != 2 || next.History.MaxExchanges != 12 {
		t.Fatalf("removed override revision=%d capacity=%d", revision, next.History.MaxExchanges)
	}
}

func TestManagerRemovesAuthenticationOverridesTogether(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := manager.Update(1, map[string]json.RawMessage{
		"authentication.username": json.RawMessage(`"operator"`),
		"authentication.password": json.RawMessage(`"secret"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	next, revision, err := manager.RemoveAuthentication(revision)
	if err != nil {
		t.Fatal(err)
	}
	if revision != 3 || next.Auth.Username != "" || next.Auth.Password != "" {
		t.Fatalf("authentication reset revision=%d auth=%+v", revision, next.Auth)
	}
	if overrides := manager.View().Overrides; len(overrides) != 0 {
		t.Fatalf("authentication overrides remain: %v", overrides)
	}
}

func TestManagerUpdatesAndRemovesOverridesAtomically(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := manager.Update(1, map[string]json.RawMessage{"capture_response.body": json.RawMessage(`"override"`)})
	if err != nil {
		t.Fatal(err)
	}
	next, revision, err := manager.UpdateWithRemovals(revision, map[string]json.RawMessage{"preview.enabled": json.RawMessage(`false`)}, []string{"capture_response.body"})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 3 || next.Capture.Body != cfg.Capture.Body || next.Preview.Enabled {
		t.Fatalf("atomic update revision=%d config=%+v", revision, next)
	}
}

func TestManagerPreparedUpdateDoesNotMutateBeforeCommit(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.Prepare(UpdateRequest{ExpectedRevision: 1, Values: map[string]json.RawMessage{
		"listeners.management": json.RawMessage(`"127.0.0.1:9080"`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	current, revision := manager.Current()
	if revision != 1 || current.Listeners.Management != cfg.Listeners.Management {
		t.Fatalf("configuration mutated before commit: revision=%d management=%q", revision, current.Listeners.Management)
	}
	if _, err := os.Stat(cfg.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared update persisted before commit: %v", err)
	}
	next, revision, err := prepared.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if revision != 2 || next.Listeners.Management != "127.0.0.1:9080" {
		t.Fatalf("committed update: revision=%d management=%q", revision, next.Listeners.Management)
	}
	if contains(manager.View().PendingRestart, "listeners.management") {
		t.Fatalf("management listener remains restart-required: %v", manager.View().PendingRestart)
	}
}

func TestManagerPreparedUpdateRejectsStaleCommit(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.Prepare(UpdateRequest{ExpectedRevision: 1, Values: map[string]json.RawMessage{"preview.enabled": json.RawMessage(`false`)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Update(1, map[string]json.RawMessage{"capture_response.body": json.RawMessage(`"newer"`)}); err != nil {
		t.Fatal(err)
	}
	if _, revision, err := prepared.Commit(); !errors.Is(err, ErrRevisionConflict) || revision != 2 {
		t.Fatalf("stale commit revision=%d error=%v", revision, err)
	}
	current, _ := manager.Current()
	if current.Capture.Body != "newer" || !current.Preview.Enabled {
		t.Fatalf("stale commit changed configuration: %+v", current)
	}
}

func TestManagerPersistenceFailureLeavesRuntimeUnchanged(t *testing.T) {
	directory := t.TempDir()
	cfg := validConfig()
	cfg.ConfigPath = filepath.Join(directory, "reqrelay.yaml")
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.ConfigPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Update(1, map[string]json.RawMessage{"capture_response.body": json.RawMessage(`"changed"`)}); err == nil {
		t.Fatal("update to directory path succeeded")
	}
	current, revision := manager.Current()
	if revision != 1 || current.Capture.Body != cfg.Capture.Body {
		t.Fatalf("failed persistence changed runtime revision=%d body=%q", revision, current.Capture.Body)
	}
}
