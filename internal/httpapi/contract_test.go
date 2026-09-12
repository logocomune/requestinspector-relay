package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
)

func TestAPIV1FrontendFixturesMatchFrozenTypes(t *testing.T) {
	tests := []struct {
		name   string
		target any
	}{
		{name: "status.json", target: &statusResponse{}},
		{name: "config.json", target: &config.ConfigurationView{}},
		{name: "exchanges.json", target: &listResponse{}},
		{name: "exchange-detail.json", target: &exchangeDetail{}},
		{name: "udp-exchange-detail.json", target: &exchangeDetail{}},
		{name: "sse-snapshot.json", target: &streamSnapshot{}},
		{name: "sse-resync.json", target: &streamResync{}},
		{name: "sse-event.json", target: &events.Envelope{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFixtureRoundTrip(t, test.name, test.target)
		})
	}
}

func TestAPIV1SchemaVersionRemainsOne(t *testing.T) {
	if events.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", events.SchemaVersion)
	}
	want := []events.Type{
		events.ExchangeStarted,
		events.RequestCompleted,
		events.ResponseCompleted,
		events.ExchangeFailed,
		events.ExchangeEvicted,
		events.ExchangeDeleted,
		events.HistoryCleared,
		events.ConfigChanged,
	}
	got := []events.Type{
		"exchange.started",
		"request.completed",
		"response.completed",
		"exchange.failed",
		"exchange.evicted",
		"exchange.deleted",
		"history.cleared",
		"config.changed",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event names = %v, want %v", got, want)
	}
}

func assertFixtureRoundTrip(t *testing.T, name string, target any) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "docs", "api-v1-fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(fixture))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("fixture contains trailing JSON: %v", err)
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var fixtureValue, encodedValue any
	if err := json.Unmarshal(fixture, &fixtureValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &encodedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixtureValue, encodedValue) {
		t.Fatalf("fixture changes after typed round trip\nfixture: %s\nencoded: %s", fixture, encoded)
	}
}
