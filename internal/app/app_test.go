package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/httpapi"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
)

func TestStartProxiesTrafficAndExposesCapturedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		if request.URL.EscapedPath() != "/api/a%2Fb" || request.URL.RawQuery != "x=1&x=2" || string(body) != "request" {
			t.Errorf("upstream request = %s body=%q", request.URL.String(), body)
		}
		writer.Header().Add("Set-Cookie", "one=1")
		writer.Header().Add("Set-Cookie", "two=2")
		writer.WriteHeader(http.StatusConflict)
		if _, err := writer.Write([]byte("upstream response")); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	cfg := testConfig()
	cfg.Mode = config.ModeProxy
	cfg.Upstream.URL = upstream.URL + "/api"
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	request, err := http.NewRequest(http.MethodPost, "http://"+running.TrafficAddress+"/a%2Fb?x=1&x=2", bytes.NewBufferString("request"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict || string(body) != "upstream response" || len(response.Header.Values("Set-Cookie")) != 2 {
		t.Fatalf("proxy response status=%d headers=%v body=%q", response.StatusCode, response.Header, body)
	}
	history := get(t, client, "http://"+running.ManagementAddress+"/api/v1/exchanges")
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(history.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if err := history.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("history items = %d", len(list.Items))
	}
	detail := get(t, client, "http://"+running.ManagementAddress+"/api/v1/exchanges/"+list.Items[0].ID)
	var document struct {
		ResponseAvailable bool `json:"response_available"`
		Exchange          struct {
			EffectiveUpstream string `json:"effective_upstream"`
			Response          struct {
				Status int    `json:"status"`
				Origin string `json:"origin"`
			} `json:"response"`
			ProxyTiming map[string]any `json:"proxy_timing"`
		} `json:"exchange"`
	}
	if err := json.NewDecoder(detail.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if err := detail.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !document.ResponseAvailable || document.Exchange.Response.Status != http.StatusConflict || document.Exchange.Response.Origin != "upstream" || document.Exchange.EffectiveUpstream == "" || len(document.Exchange.ProxyTiming) == 0 {
		t.Fatalf("detail = %+v", document)
	}
}

func TestStartAppliesCORSToHTTPIngest(t *testing.T) {
	cfg := testConfig()
	cfg.CORS.Enabled = true
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	preflight, err := http.NewRequest(http.MethodOptions, "http://"+running.TrafficAddress+"/ingest", nil)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Header.Set("Origin", "https://client.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "Content-Type")
	response, err := client.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || response.Header.Get("Access-Control-Allow-Origin") != "https://client.example" {
		t.Fatalf("preflight status=%d headers=%v", response.StatusCode, response.Header)
	}

	request, err := http.NewRequest(http.MethodPost, "http://"+running.TrafficAddress+"/ingest", strings.NewReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://client.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "https://client.example" {
		t.Fatalf("ingest CORS header = %q", response.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestSavingCORSSettingUpdatesHTTPIngest(t *testing.T) {
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(`{"expected_revision":1,"overrides":{"cors.enabled":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Fatalf("CORS setting save status=%d body=%s", response.StatusCode, body)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	preflight, err := http.NewRequest(http.MethodOptions, "http://"+running.TrafficAddress+"/ingest", nil)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Header.Set("Origin", "https://client.example")
	response, err = client.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || response.Header.Get("Access-Control-Allow-Origin") != "https://client.example" {
		t.Fatalf("saved CORS status=%d headers=%v", response.StatusCode, response.Header)
	}
}

func TestStartBindsAndClosesUDPListener(t *testing.T) {
	cfg := testConfig()
	cfg.UDP.Enabled = true
	cfg.UDP.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if running.UDPAddress == "" {
		t.Fatal("UDP listener address is empty")
	}
	address := running.UDPAddress
	cancel()
	if err := running.Wait(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenPacket("udp", address)
	if err != nil {
		t.Fatalf("UDP listener remained bound: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUDPCaptureReplyIsLimitedToDatagramSize(t *testing.T) {
	cfg := testConfig()
	cfg.UDP.Enabled, cfg.UDP.Listen, cfg.UDP.CaptureResponse = true, "127.0.0.1:0", "reply"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = running.Wait() }()
	connection, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8)
	count, err := connection.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffer[:count]) != "r" {
		t.Fatalf("reply = %q", buffer[:count])
	}
}

func TestUDPProxyKeepsClientRepliesIsolated(t *testing.T) {
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		buffer := make([]byte, 64)
		for {
			count, address, readError := upstream.ReadFromUDP(buffer)
			if readError != nil {
				return
			}
			_, _ = upstream.WriteToUDP(append([]byte("reply:"), buffer[:count]...), address)
		}
	}()
	cfg := testConfig()
	cfg.UDP.Enabled, cfg.UDP.Listen, cfg.UDP.Mode, cfg.UDP.Upstream = true, "127.0.0.1:0", config.ModeProxy, upstream.LocalAddr().String()
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = running.Wait() }()
	first, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for _, client := range []struct {
		connection net.Conn
		payload    string
	}{{first, "first"}, {second, "second"}} {
		if _, err := client.connection.Write([]byte(client.payload)); err != nil {
			t.Fatal(err)
		}
	}
	for _, client := range []struct {
		connection net.Conn
		want       string
	}{{first, "reply:first"}, {second, "reply:second"}} {
		if err := client.connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 64)
		count, err := client.connection.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(buffer[:count]); got != client.want {
			t.Fatalf("reply = %q, want %q", got, client.want)
		}
	}
}

func TestCanonicalUDPAddrUnmapsIPv4MappedIPv6(t *testing.T) {
	address, ok := canonicalUDPAddr(&net.UDPAddr{IP: net.ParseIP("::ffff:192.0.2.9"), Port: 9000})
	if !ok || address.String() != "192.0.2.9:9000" {
		t.Fatalf("canonical address = %v, valid = %t", address, ok)
	}
}

func TestUDPSessionTTLClosesSocket(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	cfg := config.Defaults().UDP
	cfg.SessionTTL = 10 * time.Millisecond
	manager := newUDPSessions(listener, upstream.LocalAddr().(*net.UDPAddr).AddrPort(), cfg)
	defer manager.Close()
	source := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 45678}
	if err := manager.Forward(source, []byte("request")); err != nil {
		t.Fatal(err)
	}
	key, ok := canonicalUDPAddr(source)
	if !ok {
		t.Fatal("invalid test source")
	}
	session, found := manager.cache.Get(key)
	if !found {
		t.Fatal("session not created")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := session.connection.Write([]byte("late")); err != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expired session socket remained writable")
}

func TestUDPSessionEvictionClosesSocket(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	cfg := config.Defaults().UDP
	cfg.MaxSessions = 1
	manager := newUDPSessions(listener, upstream.LocalAddr().(*net.UDPAddr).AddrPort(), cfg)
	defer manager.Close()
	first := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 45678}
	if err := manager.Forward(first, []byte("first")); err != nil {
		t.Fatal(err)
	}
	key, _ := canonicalUDPAddr(first)
	session, found := manager.cache.Get(key)
	if !found {
		t.Fatal("first session not created")
	}
	if err := manager.Forward(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 45679}, []byte("second")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.connection.Write([]byte("late")); err == nil {
		t.Fatal("evicted session socket remained writable")
	}
}

func TestUDPSessionsCloseConcurrentWithForward(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	manager := newUDPSessions(listener, upstream.LocalAddr().(*net.UDPAddr).AddrPort(), config.Defaults().UDP)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for index := 0; index < 100; index++ {
			_ = manager.Forward(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 45000 + index}, []byte("packet"))
		}
	}()
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
}

func TestUDPSessionReplyBudgetDropsExcessReplies(t *testing.T) {
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		buffer := make([]byte, 64)
		count, address, readError := upstream.ReadFromUDP(buffer)
		if readError != nil {
			return
		}
		_, _ = upstream.WriteToUDP([]byte("one"), address)
		_, _ = upstream.WriteToUDP([]byte("two"), address)
		_ = count
	}()
	cfg := testConfig()
	cfg.UDP.Enabled, cfg.UDP.Listen, cfg.UDP.Mode, cfg.UDP.Upstream, cfg.UDP.MaxRepliesPerSecond = true, "127.0.0.1:0", config.ModeProxy, upstream.LocalAddr().String(), 1
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = running.Wait() }()
	client, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	if _, err := client.Read(buffer); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(buffer); err == nil {
		t.Fatal("reply budget forwarded more than one reply")
	}
}

func FuzzUDPReplyBudgetStateTransitions(f *testing.F) {
	f.Add(uint8(1), uint16(1), []byte{1, 2, 3})
	f.Add(uint8(4), uint16(64), []byte{0, 255, 8, 9})
	f.Fuzz(func(t *testing.T, replyLimit uint8, byteLimit uint16, operations []byte) {
		cfg := config.Defaults().UDP
		cfg.MaxRepliesPerSecond = int(replyLimit%8) + 1
		cfg.MaxReplyBytesPerSecond = int64(byteLimit%256) + 1
		now := time.Unix(0, 0)
		manager := &udpSessions{config: cfg, now: func() time.Time { return now }}
		session := &udpSession{manager: manager, windowStart: now}
		for _, operation := range operations {
			if operation&0x80 != 0 {
				now = now.Add(time.Second)
			}
			beforeReplies, beforeBytes := session.replies, session.bytes
			reset := now.Sub(session.windowStart) >= time.Second
			reason := session.allowReply(int(operation & 0x7f))
			if reason == "" {
				expectedReplies, expectedBytes := beforeReplies+1, beforeBytes+int64(operation&0x7f)
				if reset {
					expectedReplies, expectedBytes = 1, int64(operation&0x7f)
				}
				if session.replies != expectedReplies || session.bytes != expectedBytes {
					t.Fatalf("accepted reply mutated budget incorrectly: replies=%d bytes=%d", session.replies, session.bytes)
				}
			}
			if session.replies > cfg.MaxRepliesPerSecond || session.bytes > cfg.MaxReplyBytesPerSecond {
				t.Fatalf("budget exceeded: replies=%d/%d bytes=%d/%d", session.replies, cfg.MaxRepliesPerSecond, session.bytes, cfg.MaxReplyBytesPerSecond)
			}
		}
	})
}

func TestUDPSessionReplyBudgetReasons(t *testing.T) {
	manager := &udpSessions{config: config.UDP{MaxRepliesPerSecond: 1, MaxReplyBytesPerSecond: 4}}
	rate := &udpSession{manager: manager, windowStart: time.Now()}
	if reason := rate.allowReply(1); reason != "" {
		t.Fatalf("first reply rejection = %q", reason)
	}
	if reason := rate.allowReply(1); reason != "reply_rate_limited" {
		t.Fatalf("rate rejection = %q", reason)
	}
	bytes := &udpSession{manager: manager, windowStart: time.Now()}
	if reason := bytes.allowReply(5); reason != "reply_byte_limited" {
		t.Fatalf("byte rejection = %q", reason)
	}
}

func TestUDPSessionReplyBudgetWindowUsesClock(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	manager := &udpSessions{config: config.UDP{MaxRepliesPerSecond: 1, MaxReplyBytesPerSecond: 4}, now: func() time.Time { return now }}
	session := &udpSession{manager: manager, windowStart: now}
	if reason := session.allowReply(1); reason != "" {
		t.Fatalf("first reply rejection = %q", reason)
	}
	now = now.Add(time.Second)
	if reason := session.allowReply(1); reason != "" {
		t.Fatalf("window reset rejection = %q", reason)
	}
}

func TestUDPProxyShutdownDropsLateUpstreamReply(t *testing.T) {
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	received := make(chan *net.UDPAddr, 1)
	go func() {
		buffer := make([]byte, 64)
		_, address, readError := upstream.ReadFromUDP(buffer)
		if readError == nil {
			received <- address
		}
	}()
	cfg := testConfig()
	cfg.UDP.Enabled, cfg.UDP.Listen, cfg.UDP.Mode, cfg.UDP.Upstream = true, "127.0.0.1:0", config.ModeProxy, upstream.LocalAddr().String()
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		cancel()
		_ = running.Wait()
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	var upstreamClient *net.UDPAddr
	select {
	case upstreamClient = <-received:
	case <-time.After(time.Second):
		t.Fatal("upstream did not receive proxied packet")
	}
	cancel()
	if err := running.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := upstream.WriteToUDP([]byte("late"), upstreamClient); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	if _, err := client.Read(buffer); err == nil {
		t.Fatal("late upstream reply was forwarded after shutdown")
	}
}

func TestUDPOversizedDatagramAppearsDiscarded(t *testing.T) {
	cfg := testConfig()
	cfg.UDP.Enabled, cfg.UDP.Listen, cfg.UDP.MaxDatagramBytes = true, "127.0.0.1:0", 3
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = running.Wait() }()
	connection, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("four")); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response := get(t, client, "http://"+running.ManagementAddress+"/api/v1/exchanges")
		var document struct {
			Items []struct {
				Transport string `json:"transport"`
				State     string `json:"state"`
				Error     *struct {
					Category string `json:"category"`
				} `json:"error"`
			} `json:"items"`
		}
		err = json.NewDecoder(response.Body).Decode(&document)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(document.Items) > 0 {
			if document.Items[0].Transport != "udp" || document.Items[0].State != "discarded" || document.Items[0].Error == nil || document.Items[0].Error.Category != "udp_datagram_oversized" {
				t.Fatalf("exchange = %+v", document.Items[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("UDP exchange not observed")
}

func TestStartRejectsUnresolvableUDPProxyBeforeBind(t *testing.T) {
	cfg := testConfig()
	cfg.UDP.Enabled = true
	cfg.UDP.Mode = config.ModeProxy
	cfg.UDP.Upstream = "invalid.invalid:9000"
	if _, err := Start(context.Background(), cfg, BuildInfo{Version: "test"}); err == nil || !strings.Contains(err.Error(), "resolve UDP upstream") {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestSQLiteHistorySurvivesApplicationRestart(t *testing.T) {
	cfg := testConfig()
	cfg.Storage.Mode = config.StorageSQLite
	cfg.Storage.SQLitePath = filepath.Join(t.TempDir(), "history.db")
	firstContext, stopFirst := context.WithCancel(context.Background())
	first, err := Start(firstContext, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+first.TrafficAddress+"/persisted", strings.NewReader("request body"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	stopFirst()
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}

	secondContext, stopSecond := context.WithCancel(context.Background())
	second, err := Start(secondContext, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopSecond()
		if err := second.Wait(); err != nil {
			t.Error(err)
		}
	}()
	history := get(t, http.DefaultClient, "http://"+second.ManagementAddress+"/api/v1/exchanges")
	body, err := io.ReadAll(history.Body)
	_ = history.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if history.StatusCode != http.StatusOK || !strings.Contains(string(body), `"path":"/persisted"`) {
		t.Fatalf("history status=%d body=%s", history.StatusCode, body)
	}
}

func TestUDPHistorySurvivesSQLiteRestart(t *testing.T) {
	cfg := testConfig()
	cfg.Storage.Mode, cfg.Storage.SQLitePath = config.StorageSQLite, filepath.Join(t.TempDir(), "udp.db")
	cfg.UDP.Enabled, cfg.UDP.Listen = true, "127.0.0.1:0"
	firstContext, stopFirst := context.WithCancel(context.Background())
	first, err := Start(firstContext, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.Dial("udp", first.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	_, err = connection.Write([]byte("persist UDP"))
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	stopFirst()
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	secondContext, stopSecond := context.WithCancel(context.Background())
	second, err := Start(secondContext, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { stopSecond(); _ = second.Wait() }()
	history := get(t, http.DefaultClient, "http://"+second.ManagementAddress+"/api/v1/exchanges")
	body, err := io.ReadAll(history.Body)
	_ = history.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if history.StatusCode != http.StatusOK || !strings.Contains(string(body), `"transport":"udp"`) {
		t.Fatalf("history status=%d body=%s", history.StatusCode, body)
	}
}

func TestUDPOverloadCreatesDiscardedExchange(t *testing.T) {
	originalOpen := openSQLite
	openSQLite = func(options requestsqlite.Options) (*requestsqlite.Repository, error) {
		options.QueueCount, options.FlushInterval = 1, time.Hour
		return requestsqlite.Open(options)
	}
	t.Cleanup(func() { openSQLite = originalOpen })
	cfg := testConfig()
	cfg.Storage.Mode, cfg.Storage.SQLitePath = config.StorageSQLite, filepath.Join(t.TempDir(), "overload.db")
	cfg.UDP.Enabled, cfg.UDP.Listen = true, "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = running.Wait() }()
	connection, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	_, err = connection.Write([]byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = connection.Write([]byte("two"))
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response := get(t, client, "http://"+running.ManagementAddress+"/api/v1/exchanges")
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if strings.Contains(string(body), `"udp_datagram_overload"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("discarded overload exchange not observed")
}

func TestUDPReceiveAndShutdownConcurrently(t *testing.T) {
	cfg := testConfig()
	cfg.UDP.Enabled, cfg.UDP.Listen = true, "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.Dial("udp", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 0; index < 100; index++ {
			_, _ = connection.Write([]byte("packet"))
		}
	}()
	cancel()
	if err := running.Wait(); err != nil {
		t.Fatal(err)
	}
	<-done
	_ = connection.Close()
}

func TestUDPIngressLoadProfile(t *testing.T) {
	for _, datagramsPerSecond := range []int{100, 200} {
		t.Run(strconv.Itoa(datagramsPerSecond)+"_datagrams_per_second", func(t *testing.T) {
			cfg := testConfig()
			cfg.Storage.Mode = config.StorageSQLite
			cfg.Storage.SQLitePath = filepath.Join(t.TempDir(), "udp-load.db")
			cfg.History.MaxExchanges = 256
			cfg.UDP.Enabled, cfg.UDP.Listen = true, "127.0.0.1:0"
			ctx, cancel := context.WithCancel(context.Background())
			running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
			if err != nil {
				t.Fatal(err)
			}
			connection, err := net.Dial("udp", running.UDPAddress)
			if err != nil {
				cancel()
				_ = running.Wait()
				t.Fatal(err)
			}
			payload := bytes.Repeat([]byte("p"), 1024)
			interval := time.Second / time.Duration(datagramsPerSecond)
			for sent := 0; sent < datagramsPerSecond; sent++ {
				if _, err := connection.Write(payload); err != nil {
					t.Fatal(err)
				}
				time.Sleep(interval)
			}
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}

			client := &http.Client{Transport: &http.Transport{Proxy: nil}}
			waitForUDPPersistedExchanges(t, client, running.ManagementAddress, datagramsPerSecond)
			status := get(t, client, "http://"+running.ManagementAddress+"/api/v1/status")
			var profile struct {
				RAMExchanges int `json:"ram_exchanges"`
				RAMCapacity  int `json:"ram_capacity"`
				UDP          struct {
					Sessions int `json:"sessions"`
				} `json:"udp"`
				Storage *struct {
					QueueCount    int `json:"queue_count"`
					QueueCapacity int `json:"queue_capacity"`
				} `json:"storage"`
			}
			if err := json.NewDecoder(status.Body).Decode(&profile); err != nil {
				t.Fatal(err)
			}
			if err := status.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if profile.RAMExchanges > profile.RAMCapacity || profile.UDP.Sessions != 0 || profile.Storage == nil || profile.Storage.QueueCount > profile.Storage.QueueCapacity {
				t.Fatalf("unbounded UDP load status: %+v", profile)
			}
			cancel()
			if err := running.Wait(); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(cfg.Storage.SQLitePath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() > 2<<20 {
				t.Fatalf("SQLite growth = %d bytes, want <= %d", info.Size(), 2<<20)
			}
		})
	}
}

func waitForUDPPersistedExchanges(t *testing.T, client *http.Client, managementAddress string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := get(t, client, "http://"+managementAddress+"/api/v1/exchanges?limit=200")
		var page struct {
			Items []struct {
				Transport string `json:"transport"`
			} `json:"items"`
		}
		err := json.NewDecoder(response.Body).Decode(&page)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		udpCount := 0
		for _, item := range page.Items {
			if item.Transport == "udp" {
				udpCount++
			}
		}
		if udpCount == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("persisted UDP exchanges did not reach %d", want)
}

func TestStartServesIndependentListenersAndStops(t *testing.T) {
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg.Capture.Status = http.StatusCreated
	cfg.Capture.Headers = map[string][]string{"Content-Type": {"application/json"}, "X-Capture": {"one", "two"}}
	cfg.Capture.Body = `{"captured":true}`
	ctx, cancel := context.WithCancel(context.Background())
	runtime, err := Start(ctx, cfg, BuildInfo{Version: "test-build", Commit: "test-commit", Date: "2026-09-08T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	health := get(t, client, "http://"+runtime.ManagementAddress+"/api/v1/health")
	if health.StatusCode != http.StatusOK || health.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("health response status=%d content-type=%q", health.StatusCode, health.Header.Get("Content-Type"))
	}
	var healthBody map[string]string
	if err := json.NewDecoder(health.Body).Decode(&healthBody); err != nil {
		t.Fatal(err)
	}
	if err := health.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if healthBody["status"] != "ok" {
		t.Fatalf("health status = %q", healthBody["status"])
	}
	status := get(t, client, "http://"+runtime.ManagementAddress+"/api/v1/status")
	var statusBody map[string]any
	if err := json.NewDecoder(status.Body).Decode(&statusBody); err != nil {
		t.Fatal(err)
	}
	if err := status.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if statusBody["build"] != "test-build" || statusBody["commit"] != "test-commit" || statusBody["build_date"] != "2026-09-08T12:00:00Z" || statusBody["mode"] != config.ModeCapture {
		t.Fatalf("unexpected status response: %v", statusBody)
	}

	index := get(t, client, "http://"+runtime.ManagementAddress+"/")
	if index.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", index.StatusCode)
	}
	if err := index.Body.Close(); err != nil {
		t.Fatal(err)
	}
	traffic := get(t, client, "http://"+runtime.TrafficAddress+"/anything")
	trafficBody, err := io.ReadAll(traffic.Body)
	if err != nil {
		t.Fatal(err)
	}
	if traffic.StatusCode != http.StatusCreated || string(trafficBody) != cfg.Capture.Body || len(traffic.Header.Values("X-Capture")) != 2 {
		t.Fatalf("traffic response status=%d headers=%v body=%q", traffic.StatusCode, traffic.Header, trafficBody)
	}
	if err := traffic.Body.Close(); err != nil {
		t.Fatal(err)
	}
	history := get(t, client, "http://"+runtime.ManagementAddress+"/api/v1/exchanges")
	historyData, err := io.ReadAll(history.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if history.StatusCode != http.StatusOK || !strings.Contains(string(historyData), `"request_body_bytes":0`) || strings.Contains(string(historyData), cfg.Capture.Body) {
		t.Fatalf("history status=%d body=%s", history.StatusCode, historyData)
	}
	update, err := http.NewRequest(http.MethodPut, "http://"+runtime.ManagementAddress+"/api/v1/config", strings.NewReader(`{"expected_revision":1,"overrides":{"capture_response.body":"updated"}}`))
	if err != nil {
		t.Fatal(err)
	}
	updateResponse, err := client.Do(update)
	if err != nil {
		t.Fatal(err)
	}
	if updateResponse.StatusCode != http.StatusOK {
		data, readError := io.ReadAll(updateResponse.Body)
		if readError != nil {
			t.Fatal(readError)
		}
		t.Fatalf("config update status=%d body=%s", updateResponse.StatusCode, data)
	}
	if err := updateResponse.Body.Close(); err != nil {
		t.Fatal(err)
	}
	updatedTraffic := get(t, client, "http://"+runtime.TrafficAddress+"/updated")
	updatedBody, err := io.ReadAll(updatedTraffic.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := updatedTraffic.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(updatedBody) != "updated" {
		t.Fatalf("updated capture body = %q", updatedBody)
	}

	cancel()
	if err := runtime.Wait(); err != nil {
		t.Fatal(err)
	}
	assertAddressReusable(t, runtime.ManagementAddress)
	assertAddressReusable(t, runtime.TrafficAddress)
}

func TestManagementListenerReloadsWithoutStoppingTraffic(t *testing.T) {
	cfg := testConfig()
	temporaryDirectory := t.TempDir()
	cfg.ConfigPath = filepath.Join(temporaryDirectory, "reqrelay.yaml")
	cfg.Storage.Mode = config.StorageSQLite
	cfg.Storage.SQLitePath = filepath.Join(temporaryDirectory, "reqrelay.db")
	cfg.UDP.Enabled = true
	cfg.UDP.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			cancel()
			if err := running.Wait(); err != nil {
				t.Error(err)
			}
		}
	}()

	newManagementAddress := reserveAddress(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	streamResponse, err := client.Get("http://" + running.ManagementAddress + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	streamClosed := make(chan error, 1)
	go func() {
		_, readError := io.Copy(io.Discard, streamResponse.Body)
		streamClosed <- readError
	}()
	updateBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"listeners.management":%q}}`, newManagementAddress)
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(data), `"management_reload"`) {
		t.Fatalf("reload status=%d body=%s", response.StatusCode, data)
	}
	select {
	case <-streamClosed:
		_ = streamResponse.Body.Close()
	case <-time.After(time.Second):
		_ = streamResponse.Body.Close()
		t.Fatal("previous management SSE stream remains open")
	}

	deadline := time.Now().Add(time.Second)
	for {
		response, err = client.Get("http://" + newManagementAddress + "/api/v1/health")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new management listener unavailable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	oldListenerDeadline := time.Now().Add(time.Second)
	for {
		response, err = client.Get("http://" + running.ManagementAddress + "/api/v1/health")
		if err != nil {
			break
		}
		_ = response.Body.Close()
		if time.Now().After(oldListenerDeadline) {
			t.Fatal("old management listener remains available")
		}
		time.Sleep(10 * time.Millisecond)
	}
	traffic := get(t, client, "http://"+running.TrafficAddress+"/after-management-reload")
	if traffic.StatusCode != http.StatusOK {
		t.Fatalf("traffic status after management reload = %d", traffic.StatusCode)
	}
	if err := traffic.Body.Close(); err != nil {
		t.Fatal(err)
	}
	udpAddress, err := net.ResolveUDPAddr("udp4", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	udpConnection, err := net.DialUDP("udp4", nil, udpAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := udpConnection.Write([]byte("after-management-reload")); err != nil {
		t.Fatal(err)
	}
	_ = udpConnection.Close()
	waitForUDPPersistedExchanges(t, client, newManagementAddress, 1)
	assertAddressReusable(t, running.ManagementAddress)
	cancel()
	if err := running.Wait(); err != nil {
		t.Fatal(err)
	}
	stopped = true
	assertAddressReusable(t, newManagementAddress)
	assertAddressReusable(t, running.TrafficAddress)
}

func TestTrafficListenerReloadsWithoutStoppingManagement(t *testing.T) {
	cfg := testConfig()
	temporaryDirectory := t.TempDir()
	cfg.ConfigPath = filepath.Join(temporaryDirectory, "reqrelay.yaml")
	cfg.Storage.Mode = config.StorageSQLite
	cfg.Storage.SQLitePath = filepath.Join(temporaryDirectory, "reqrelay.db")
	cfg.UDP.Enabled = true
	cfg.UDP.Listen = "127.0.0.1:00"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	newTrafficAddress := reserveAddress(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	updateBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"listeners.traffic":%q}}`, newTrafficAddress)
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(data), `"traffic_reload"`) {
		t.Fatalf("reload status=%d body=%s", response.StatusCode, data)
	}

	traffic := get(t, client, "http://"+newTrafficAddress+"/after-traffic-reload")
	if traffic.StatusCode != http.StatusOK {
		t.Fatalf("traffic status after reload = %d", traffic.StatusCode)
	}
	if err := traffic.Body.Close(); err != nil {
		t.Fatal(err)
	}
	status := get(t, client, "http://"+running.ManagementAddress+"/api/v1/status")
	statusData, err := io.ReadAll(status.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := status.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if status.StatusCode != http.StatusOK || !strings.Contains(string(statusData), `"traffic_listener":"`+newTrafficAddress+`"`) {
		t.Fatalf("status after traffic reload status=%d body=%s", status.StatusCode, statusData)
	}
	udpAddress, err := net.ResolveUDPAddr("udp4", running.UDPAddress)
	if err != nil {
		t.Fatal(err)
	}
	udpConnection, err := net.DialUDP("udp4", nil, udpAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := udpConnection.Write([]byte("after-traffic-reload")); err != nil {
		t.Fatal(err)
	}
	_ = udpConnection.Close()
	waitForUDPPersistedExchanges(t, client, running.ManagementAddress, 1)
	assertAddressReusable(t, running.TrafficAddress)
}

func TestUDPListenerReloadsWithoutStoppingHTTP(t *testing.T) {
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg.UDP.Enabled = true
	cfg.UDP.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	oldUDPAddress := running.UDPAddress
	newUDPAddress := reserveUDPAddress(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	updateBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"udp.listen":%q}}`, newUDPAddress)
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(data), `"udp_reload"`) || !strings.Contains(string(data), newUDPAddress) {
		t.Fatalf("reload status=%d body=%s", response.StatusCode, data)
	}

	sendUDP(t, newUDPAddress, "after-udp-reload")
	waitForUDPPersistedExchanges(t, client, running.ManagementAddress, 1)
	traffic := get(t, client, "http://"+running.TrafficAddress+"/after-udp-reload")
	if traffic.StatusCode != http.StatusOK {
		t.Fatalf("traffic status after UDP reload = %d", traffic.StatusCode)
	}
	if err := traffic.Body.Close(); err != nil {
		t.Fatal(err)
	}
	assertUDPAddressReusable(t, oldUDPAddress)
}

func TestUDPListenerBindFailureKeepsConfigurationAndEndpoint(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg.UDP.Enabled = true
	cfg.UDP.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	updateBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"udp.listen":%q,"udp.capture_response":"must-not-apply"}}`, occupied.LocalAddr().String())
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(data), `"code":"udp_listener_unavailable"`) {
		t.Fatalf("bind failure status=%d body=%s", response.StatusCode, data)
	}
	if !strings.Contains(logs.String(), occupied.LocalAddr().String()) || strings.Contains(logs.String(), "must-not-apply") {
		t.Fatalf("reload log=%q", logs.String())
	}
	sendUDP(t, running.UDPAddress, "after-rejected-udp-reload")
	waitForUDPPersistedExchanges(t, client, running.ManagementAddress, 1)
	configuration := get(t, client, "http://"+running.ManagementAddress+"/api/v1/config")
	configurationData, err := io.ReadAll(configuration.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = configuration.Body.Close()
	if configuration.StatusCode != http.StatusOK || !strings.Contains(string(configurationData), `"revision":1`) || strings.Contains(string(configurationData), "must-not-apply") {
		t.Fatalf("configuration after bind failure status=%d body=%s", configuration.StatusCode, configurationData)
	}
}

func TestUDPReloadEnablesUpdatesAndDisablesIngest(t *testing.T) {
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg.UDP.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	udpAddress := reserveUDPAddress(t)
	enableBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"udp.enabled":true,"udp.listen":%q}}`, udpAddress)
	enabled := putConfiguration(t, client, running.ManagementAddress, enableBody)
	if enabled.StatusCode != http.StatusOK || !strings.Contains(readAndClose(t, enabled), `"udp_reload"`) {
		t.Fatalf("enable UDP status = %d", enabled.StatusCode)
	}

	connection, err := net.Dial("udp", udpAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	updateReply := putConfiguration(t, client, running.ManagementAddress, `{"expected_revision":2,"overrides":{"udp.capture_response":"pong"}}`)
	updateData := readAndClose(t, updateReply)
	if updateReply.StatusCode != http.StatusOK || !strings.Contains(updateData, `"udp_reload"`) || strings.Contains(updateData, `"address_changed":true`) {
		t.Fatalf("update UDP status=%d body=%s", updateReply.StatusCode, updateData)
	}
	if _, err := connection.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8)
	count, err := connection.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffer[:count]) != "pong" {
		t.Fatalf("capture reply = %q", buffer[:count])
	}

	disabled := putConfiguration(t, client, running.ManagementAddress, `{"expected_revision":3,"overrides":{"udp.enabled":false}}`)
	disableData := readAndClose(t, disabled)
	if disabled.StatusCode != http.StatusOK || !strings.Contains(disableData, `"udp_reload":{"enabled":false`) {
		t.Fatalf("disable UDP status=%d body=%s", disabled.StatusCode, disableData)
	}
	assertUDPAddressReusable(t, udpAddress)
}

func TestTrafficListenerBindFailureKeepsConfigurationAndEndpoint(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	updateBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"listeners.traffic":%q,"capture_response.body":"must-not-apply"}}`, occupied.Addr().String())
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(data), `"code":"traffic_listener_unavailable"`) {
		t.Fatalf("bind failure status=%d body=%s", response.StatusCode, data)
	}
	if !strings.Contains(logs.String(), occupied.Addr().String()) || strings.Contains(logs.String(), "must-not-apply") {
		t.Fatalf("reload log=%q", logs.String())
	}
	configuration := get(t, client, "http://"+running.ManagementAddress+"/api/v1/config")
	configurationData, err := io.ReadAll(configuration.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := configuration.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if configuration.StatusCode != http.StatusOK || !strings.Contains(string(configurationData), `"revision":1`) || strings.Contains(string(configurationData), "must-not-apply") {
		t.Fatalf("configuration after bind failure status=%d body=%s", configuration.StatusCode, configurationData)
	}
	traffic := get(t, client, "http://"+running.TrafficAddress+"/after-rejected-reload")
	trafficData, err := io.ReadAll(traffic.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = traffic.Body.Close()
	if traffic.StatusCode != http.StatusOK || string(trafficData) != cfg.Capture.Body {
		t.Fatalf("traffic after bind failure status=%d body=%s", traffic.StatusCode, trafficData)
	}
	if _, err := os.Stat(cfg.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration file after bind failure error=%v", err)
	}
}

func TestTrafficPlaneSerializesReloadsAndShutdown(t *testing.T) {
	cfg := testConfig()
	initialListener, err := net.Listen("tcp", cfg.Listeners.Traffic)
	if err != nil {
		t.Fatal(err)
	}
	initialAddress := initialListener.Addr().String()
	plane := newTrafficPlane(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	plane.Start(initialListener, cfg)

	firstConfig := cfg
	firstConfig.Listeners.Traffic = reserveAddress(t)
	firstPlan, err := plane.Prepare(firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig := cfg
	secondConfig.Listeners.Traffic = reserveAddress(t)
	secondPlanResult := make(chan httpapi.TrafficReloadPlan, 1)
	secondPlanError := make(chan error, 1)
	go func() {
		plan, prepareError := plane.Prepare(secondConfig)
		if prepareError != nil {
			secondPlanError <- prepareError
			return
		}
		secondPlanResult <- plan
	}()
	select {
	case <-secondPlanResult:
		t.Fatal("concurrent traffic reload bypassed transition lock")
	case err := <-secondPlanError:
		t.Fatal(err)
	case <-time.After(20 * time.Millisecond):
	}
	firstPlan.Activate()

	var secondPlan httpapi.TrafficReloadPlan
	select {
	case secondPlan = <-secondPlanResult:
	case err := <-secondPlanError:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("second traffic reload did not proceed")
	}
	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- plane.Close() }()
	select {
	case err := <-shutdownResult:
		t.Fatalf("shutdown completed before prepared reload ended: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	secondPlan.Abort()
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("traffic shutdown deadlocked after reload abort")
	}
	assertAddressReusable(t, initialAddress)
	assertAddressReusable(t, firstConfig.Listeners.Traffic)
	assertAddressReusable(t, secondConfig.Listeners.Traffic)
}

func TestUDPPlaneSerializesReloadsAndShutdown(t *testing.T) {
	cfg := testConfig()
	cfg.UDP.Enabled = false
	plane := newUDPPlane(udpPlaneOptions{Context: context.Background()})
	plane.Start(nil, netip.AddrPort{}, cfg.UDP)

	firstConfig := cfg
	firstConfig.UDP.Enabled = true
	firstConfig.UDP.Listen = reserveUDPAddress(t)
	firstPlan, err := plane.Prepare(firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig := firstConfig
	secondConfig.UDP.Listen = reserveUDPAddress(t)
	secondPlanResult := make(chan httpapi.UDPReloadPlan, 1)
	secondPlanError := make(chan error, 1)
	go func() {
		plan, prepareError := plane.Prepare(secondConfig)
		if prepareError != nil {
			secondPlanError <- prepareError
			return
		}
		secondPlanResult <- plan
	}()
	select {
	case <-secondPlanResult:
		t.Fatal("concurrent UDP reload bypassed transition lock")
	case err := <-secondPlanError:
		t.Fatal(err)
	case <-time.After(20 * time.Millisecond):
	}
	firstPlan.Activate()

	var secondPlan httpapi.UDPReloadPlan
	select {
	case secondPlan = <-secondPlanResult:
	case err := <-secondPlanError:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("second UDP reload did not proceed")
	}
	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- plane.Close() }()
	select {
	case err := <-shutdownResult:
		t.Fatalf("shutdown completed before prepared UDP reload ended: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	secondPlan.Abort()
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("UDP shutdown deadlocked after reload abort")
	}
	assertUDPAddressReusable(t, firstConfig.UDP.Listen)
	assertUDPAddressReusable(t, secondConfig.UDP.Listen)
}

func TestTrafficPlaneDrainsAcceptedRequests(t *testing.T) {
	cfg := testConfig()
	initialListener, err := net.Listen("tcp", cfg.Listeners.Traffic)
	if err != nil {
		t.Fatal(err)
	}
	initialAddress := initialListener.Addr().String()
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	plane := newTrafficPlane(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/slow" {
			close(requestStarted)
			<-releaseRequest
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	plane.Start(initialListener, cfg)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	requestResult := make(chan error, 1)
	go func() {
		response, requestError := client.Get("http://" + initialAddress + "/slow")
		if requestError == nil {
			_ = response.Body.Close()
		}
		requestResult <- requestError
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach initial traffic generation")
	}

	next := cfg
	next.Listeners.Traffic = reserveAddress(t)
	plan, err := plane.Prepare(next)
	if err != nil {
		t.Fatal(err)
	}
	plan.Activate()
	response := get(t, client, "http://"+next.Listeners.Traffic+"/new-generation")
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("new generation status = %d", response.StatusCode)
	}
	select {
	case err := <-requestResult:
		t.Fatalf("accepted request ended before release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseRequest)
	select {
	case err := <-requestResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("accepted request did not finish during drain")
	}
	if err := plane.Close(); err != nil {
		t.Fatal(err)
	}
	assertAddressReusable(t, initialAddress)
	assertAddressReusable(t, next.Listeners.Traffic)
}

func TestManagementListenerBindFailureKeepsConfigurationAndEndpoint(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()

	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	updateBody := fmt.Sprintf(`{"expected_revision":1,"overrides":{"listeners.management":%q,"capture_response.body":"must-not-apply"}}`, occupied.Addr().String())
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(data), `"code":"management_listener_unavailable"`) {
		t.Fatalf("bind failure status=%d body=%s", response.StatusCode, data)
	}
	if !strings.Contains(logs.String(), occupied.Addr().String()) || strings.Contains(logs.String(), "must-not-apply") {
		t.Fatalf("reload log=%q", logs.String())
	}
	configuration := get(t, client, "http://"+running.ManagementAddress+"/api/v1/config")
	configurationData, err := io.ReadAll(configuration.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := configuration.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if configuration.StatusCode != http.StatusOK || !strings.Contains(string(configurationData), `"revision":1`) || strings.Contains(string(configurationData), "must-not-apply") {
		t.Fatalf("configuration after bind failure status=%d body=%s", configuration.StatusCode, configurationData)
	}
	if _, err := os.Stat(cfg.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration file after bind failure error=%v", err)
	}
}

func TestManagementPlaneSerializesReloadsAndShutdown(t *testing.T) {
	cfg := testConfig()
	initialListener, err := net.Listen("tcp", cfg.Listeners.Management)
	if err != nil {
		t.Fatal(err)
	}
	initialAddress := initialListener.Addr().String()
	plane := newManagementPlane(func(_ string, _ <-chan struct{}) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		})
	})
	plane.Start(initialListener, cfg)

	firstConfig := cfg
	firstConfig.Listeners.Management = reserveAddress(t)
	firstPlan, err := plane.Prepare(firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig := cfg
	secondConfig.Listeners.Management = reserveAddress(t)
	secondPlanResult := make(chan httpapi.ManagementReloadPlan, 1)
	secondPlanError := make(chan error, 1)
	go func() {
		plan, prepareError := plane.Prepare(secondConfig)
		if prepareError != nil {
			secondPlanError <- prepareError
			return
		}
		secondPlanResult <- plan
	}()
	select {
	case <-secondPlanResult:
		t.Fatal("concurrent management reload bypassed transition lock")
	case err := <-secondPlanError:
		t.Fatal(err)
	case <-time.After(20 * time.Millisecond):
	}
	firstPlan.Activate()

	var secondPlan httpapi.ManagementReloadPlan
	select {
	case secondPlan = <-secondPlanResult:
	case err := <-secondPlanError:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("second management reload did not proceed")
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- plane.Shutdown(shutdownContext) }()
	select {
	case err := <-shutdownResult:
		t.Fatalf("shutdown completed before prepared reload ended: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	secondPlan.Abort()
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("management shutdown deadlocked after reload abort")
	}
	assertAddressReusable(t, initialAddress)
	assertAddressReusable(t, firstConfig.Listeners.Management)
	assertAddressReusable(t, secondConfig.Listeners.Management)
}

func TestAuthenticationReloadInvalidatesSessionsAndUsesNewCredentials(t *testing.T) {
	cfg := testConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "reqrelay.yaml")
	cfg.Auth = config.Authentication{Username: "admin", Password: "secret", SessionTTL: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := running.Wait(); err != nil {
			t.Error(err)
		}
	}()
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	login := postJSON(t, jsonRequest{client: client, url: "http://" + running.ManagementAddress + "/api/v1/session", body: `{"username":"admin","password":"secret"}`})
	if login.StatusCode != http.StatusOK || len(login.Cookies()) != 1 {
		t.Fatalf("login status=%d cookies=%v", login.StatusCode, login.Cookies())
	}
	cookie := login.Cookies()[0]
	_ = login.Body.Close()
	updateBody := `{"expected_revision":1,"overrides":{"authentication.username":"operator","authentication.password":"replacement"}}`
	request, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(data), `"relogin_required":true`) {
		t.Fatalf("authentication reload status=%d body=%s", response.StatusCode, data)
	}
	statusRequest, err := http.NewRequest(http.MethodGet, "http://"+running.ManagementAddress+"/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	statusRequest.AddCookie(cookie)
	statusResponse, err := client.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = statusResponse.Body.Close()
	if statusResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old session status = %d", statusResponse.StatusCode)
	}
	oldLogin := postJSON(t, jsonRequest{client: client, url: "http://" + running.ManagementAddress + "/api/v1/session", body: `{"username":"admin","password":"secret"}`})
	_ = oldLogin.Body.Close()
	if oldLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old credentials status = %d", oldLogin.StatusCode)
	}
	newLogin := postJSON(t, jsonRequest{client: client, url: "http://" + running.ManagementAddress + "/api/v1/session", body: `{"username":"operator","password":"replacement"}`})
	if newLogin.StatusCode != http.StatusOK {
		t.Fatalf("new credentials status = %d", newLogin.StatusCode)
	}
	newCookie := newLogin.Cookies()[0]
	_ = newLogin.Body.Close()
	ttlRequest, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(`{"expected_revision":2,"overrides":{"authentication.session_ttl":"2h"}}`))
	if err != nil {
		t.Fatal(err)
	}
	ttlRequest.AddCookie(newCookie)
	ttlResponse, err := client.Do(ttlRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = ttlResponse.Body.Close()
	if ttlResponse.StatusCode != http.StatusOK {
		t.Fatalf("session TTL reload status = %d", ttlResponse.StatusCode)
	}
	statusRequest, err = http.NewRequest(http.MethodGet, "http://"+running.ManagementAddress+"/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	statusRequest.AddCookie(newCookie)
	statusResponse, err = client.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = statusResponse.Body.Close()
	if statusResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after TTL reload status = %d", statusResponse.StatusCode)
	}
	latestLogin := postJSON(t, jsonRequest{client: client, url: "http://" + running.ManagementAddress + "/api/v1/session", body: `{"username":"operator","password":"replacement"}`})
	if latestLogin.StatusCode != http.StatusOK || len(latestLogin.Cookies()) != 1 {
		t.Fatalf("latest login status=%d cookies=%v", latestLogin.StatusCode, latestLogin.Cookies())
	}
	latestCookie := latestLogin.Cookies()[0]
	_ = latestLogin.Body.Close()
	disableRequest, err := http.NewRequest(http.MethodPut, "http://"+running.ManagementAddress+"/api/v1/config", strings.NewReader(`{"expected_revision":3,"overrides":{"authentication.username":"","authentication.password":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	disableRequest.AddCookie(latestCookie)
	disableResponse, err := client.Do(disableRequest)
	if err != nil {
		t.Fatal(err)
	}
	disableData, err := io.ReadAll(disableResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = disableResponse.Body.Close()
	if disableResponse.StatusCode != http.StatusOK || !strings.Contains(string(disableData), `"relogin_required":false`) {
		t.Fatalf("authentication disable status=%d body=%s", disableResponse.StatusCode, disableData)
	}
	openStatus := get(t, client, "http://"+running.ManagementAddress+"/api/v1/status")
	_ = openStatus.Body.Close()
	if openStatus.StatusCode != http.StatusOK {
		t.Fatalf("status after authentication disable = %d", openStatus.StatusCode)
	}
	resetRequest, err := http.NewRequest(http.MethodDelete, "http://"+running.ManagementAddress+"/api/v1/config/overrides/authentication?expected_revision=4", nil)
	if err != nil {
		t.Fatal(err)
	}
	resetResponse, err := client.Do(resetRequest)
	if err != nil {
		t.Fatal(err)
	}
	resetData, err := io.ReadAll(resetResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resetResponse.Body.Close()
	if resetResponse.StatusCode != http.StatusOK || !strings.Contains(string(resetData), `"relogin_required":true`) {
		t.Fatalf("authentication reset status=%d body=%s", resetResponse.StatusCode, resetData)
	}
	restoredLogin := postJSON(t, jsonRequest{client: client, url: "http://" + running.ManagementAddress + "/api/v1/session", body: `{"username":"admin","password":"secret"}`})
	_ = restoredLogin.Body.Close()
	if restoredLogin.StatusCode != http.StatusOK {
		t.Fatalf("restored credentials status = %d", restoredLogin.StatusCode)
	}
}

func TestStartReportsManagementListenFailure(t *testing.T) {
	cfg := testConfig()
	cfg.Listeners.Management = "invalid"
	if _, err := Start(context.Background(), cfg, BuildInfo{Version: "test"}); err == nil || !strings.Contains(err.Error(), "management traffic") {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestStartDoesNotLeakServerGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	running, err := Start(ctx, testConfig(), BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := running.Wait(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runtime.Gosched()
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines after shutdown = %d, baseline = %d", runtime.NumGoroutine(), baseline)
}

func TestShutdownRuntimeForceClosesTrafficBeforePersistenceAndManagement(t *testing.T) {
	calls := make([]string, 0, 3)
	trafficError := errors.New("traffic close")
	persistenceError := errors.New("persistence shutdown")
	managementError := errors.New("management shutdown")
	targets := shutdownTargets{
		traffic:     &recordingServerShutdown{name: "traffic", calls: &calls, err: trafficError},
		persistence: &recordingPersistenceShutdown{name: "persistence", calls: &calls, err: persistenceError},
		management:  &recordingServerShutdown{name: "management", calls: &calls, err: managementError},
	}

	err := shutdownRuntime(context.Background(), targets)
	if got, want := strings.Join(calls, ","), "traffic-close,persistence,management-shutdown"; got != want {
		t.Fatalf("shutdown order = %q, want %q", got, want)
	}
	for _, want := range []error{trafficError, persistenceError, managementError} {
		if !errors.Is(err, want) {
			t.Errorf("shutdown error %v missing from %v", want, err)
		}
	}
}

func TestShutdownRuntimeFlushesPersistenceAfterTrafficTimeout(t *testing.T) {
	shutdownContext, cancel := context.WithCancel(context.Background())
	cancel()
	persistenceReceivedCancelledContext := false
	targets := shutdownTargets{
		traffic:     &recordingServerShutdown{},
		persistence: &recordingPersistenceShutdown{contextCancelled: &persistenceReceivedCancelledContext},
		management:  &recordingServerShutdown{},
	}

	if err := shutdownRuntime(shutdownContext, targets); err != nil {
		t.Fatal(err)
	}
	if persistenceReceivedCancelledContext {
		t.Fatal("persistence received cancelled shutdown context")
	}
}

func TestShutdownRuntimeWithoutPersistence(t *testing.T) {
	calls := make([]string, 0, 2)
	targets := shutdownTargets{
		traffic:    &recordingServerShutdown{name: "traffic", calls: &calls},
		management: &recordingServerShutdown{name: "management", calls: &calls},
	}

	if err := shutdownRuntime(context.Background(), targets); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls, ","), "traffic-close,management-shutdown"; got != want {
		t.Fatalf("shutdown order = %q, want %q", got, want)
	}
}

func TestStartClosesManagementListenerWhenTrafficListenFails(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	managementAddress := reserveAddress(t)
	cfg := testConfig()
	cfg.Listeners.Management = managementAddress
	cfg.Listeners.Traffic = occupied.Addr().String()
	if _, err := Start(context.Background(), cfg, BuildInfo{Version: "test"}); err == nil || !strings.Contains(err.Error(), "inspected traffic") {
		t.Fatalf("Start() error = %v", err)
	}
	assertAddressReusable(t, managementAddress)
}

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

type jsonRequest struct {
	client *http.Client
	url    string
	body   string
	cookie *http.Cookie
}

func postJSON(t *testing.T, input jsonRequest) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, input.url, strings.NewReader(input.body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if input.cookie != nil {
		request.AddCookie(input.cookie)
	}
	response, err := input.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func putConfiguration(t *testing.T, client *http.Client, managementAddress, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPut, "http://"+managementAddress+"/api/v1/config", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readAndClose(t *testing.T, response *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertAddressReusable(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("address %q not reusable: %v", address, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func reserveUDPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.LocalAddr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func assertUDPAddressReusable(t *testing.T, address string) {
	t.Helper()
	listener, err := net.ListenPacket("udp", address)
	if err != nil {
		t.Fatalf("UDP address %q not reusable: %v", address, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func sendUDP(t *testing.T, address, payload string) {
	t.Helper()
	connection, err := net.Dial("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
}

type recordingServerShutdown struct {
	name           string
	calls          *[]string
	err            error
	waitForContext bool
}

func (shutdown *recordingServerShutdown) Shutdown(ctx context.Context) error {
	if shutdown.calls != nil {
		*shutdown.calls = append(*shutdown.calls, shutdown.name+"-shutdown")
	}
	if shutdown.waitForContext {
		<-ctx.Done()
	}
	return shutdown.err
}

func (shutdown *recordingServerShutdown) Close() error {
	if shutdown.calls != nil {
		*shutdown.calls = append(*shutdown.calls, shutdown.name+"-close")
	}
	return shutdown.err
}

type recordingPersistenceShutdown struct {
	name             string
	calls            *[]string
	err              error
	contextCancelled *bool
}

func (shutdown *recordingPersistenceShutdown) Close(ctx context.Context) error {
	if shutdown.calls != nil {
		*shutdown.calls = append(*shutdown.calls, shutdown.name)
	}
	if shutdown.contextCancelled != nil {
		*shutdown.contextCancelled = ctx.Err() != nil
	}
	return shutdown.err
}

func testConfig() config.Config {
	cfg := config.Defaults()
	cfg.Listeners = config.Listeners{Management: "127.0.0.1:0", Traffic: "localhost:0"}
	cfg.Storage.Mode = config.StorageMemory
	cfg.Shutdown = time.Second
	return cfg
}
