package main

import (
	"log"
	"slices"
	"testing"

	"github.com/logocomune/requestinspector-relay/internal/app"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

func TestConfigureLoggingKeepsTimestamp(t *testing.T) {
	previousFlags := log.Flags()
	t.Cleanup(func() { log.SetFlags(previousFlags) })
	log.SetFlags(log.LstdFlags)

	configureLogging()

	if flags := log.Flags(); flags != log.LstdFlags {
		t.Fatalf("log flags = %d, want %d", flags, log.LstdFlags)
	}
}

func TestStartupBanner(t *testing.T) {
	runtime := &app.Runtime{
		ManagementAddress: "127.0.0.1:9080",
		TrafficAddress:    "127.0.0.1:9081",
	}

	want := "+--------------------------------------------------+\n" +
		"|  REQUESTINSPECTOR RELAY                          |\n" +
		"|  HTTP REQUEST INSPECTOR / REVERSE PROXY          |\n" +
		"+--------------------------------------------------+\n" +
		"VERSION  test-build\n" +
		"BUILT    2026-09-08T12:00:00Z\n" +
		"Ingest: http://127.0.0.1:9081\n" +
		"Web interface: http://127.0.0.1:9080\n"
	if got := startupBanner("test-build", "2026-09-08T12:00:00Z", runtime); got != want {
		t.Fatalf("startupBanner() = %q, want %q", got, want)
	}
}

func TestUDPStartupMessages(t *testing.T) {
	runtime := &app.Runtime{UDPAddress: "127.0.0.1:1799"}

	t.Run("capture", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.UDP.Enabled = true

		want := []string{"UDP listener: 127.0.0.1:1799"}
		if got := udpStartupMessages(cfg, runtime); !slices.Equal(got, want) {
			t.Fatalf("udpStartupMessages() = %q, want %q", got, want)
		}
	})

	t.Run("proxy", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.UDP.Enabled = true
		cfg.UDP.Mode = config.ModeProxy
		cfg.UDP.Upstream = "127.0.0.1:1800"

		want := []string{"UDP listener: 127.0.0.1:1799", "UDP proxy upstream: 127.0.0.1:1800"}
		if got := udpStartupMessages(cfg, runtime); !slices.Equal(got, want) {
			t.Fatalf("udpStartupMessages() = %q, want %q", got, want)
		}
	})
}
