package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	"github.com/logocomune/requestinspector-relay/internal/httpapi"
	"github.com/logocomune/requestinspector-relay/internal/proxy"
	requestsqlite "github.com/logocomune/requestinspector-relay/internal/sqlite"
	"github.com/logocomune/requestinspector-relay/internal/store"
	"github.com/logocomune/requestinspector-relay/internal/webui"
)

type Runtime struct {
	ManagementAddress string
	TrafficAddress    string
	UDPAddress        string
	done              chan error
}

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type shutdownServer interface {
	Shutdown(context.Context) error
}

type shutdownPersistence interface {
	Close(context.Context) error
}

type shutdownTargets struct {
	traffic     ioCloser
	persistence shutdownPersistence
	management  shutdownServer
	udp         ioCloser
}

var openSQLite = requestsqlite.Open

type ioCloser interface{ Close() error }

type udpResources struct {
	sessions *udpSessions
	listener *net.UDPConn
}

func (resources udpResources) Close() error {
	var sessionError error
	if resources.sessions != nil {
		sessionError = resources.sessions.Close()
	}
	return errors.Join(sessionError, resources.listener.Close())
}

func Start(ctx context.Context, cfg config.Config, build BuildInfo) (*Runtime, error) {
	staticHandler, err := webui.Handler()
	if err != nil {
		return nil, fmt.Errorf("load embedded interface: %w", err)
	}
	udpListener, upstream, err := startUDP(ctx, cfg.UDP)
	if err != nil {
		return nil, err
	}
	managementListener, err := net.Listen("tcp", cfg.Listeners.Management)
	if err != nil {
		if udpListener != nil {
			_ = udpListener.Close()
		}
		return nil, fmt.Errorf("listen for management traffic: %w", err)
	}
	trafficListener, err := net.Listen("tcp", cfg.Listeners.Traffic)
	if err != nil {
		_ = managementListener.Close()
		if udpListener != nil {
			_ = udpListener.Close()
		}
		return nil, fmt.Errorf("listen for inspected traffic: %w", err)
	}

	started := time.Now()
	configuration, err := config.NewManager(cfg)
	if err != nil {
		_ = managementListener.Close()
		_ = trafficListener.Close()
		if udpListener != nil {
			_ = udpListener.Close()
		}
		return nil, fmt.Errorf("initialize runtime configuration: %w", err)
	}
	repository := store.NewMemory(cfg.History.MaxExchanges)
	eventBus := events.NewBus(events.Limits{
		ReplayCount:     1_000,
		ReplayBytes:     8 << 20,
		SubscriberCount: 256,
		SubscriberBytes: 2 << 20,
	}, time.Now)
	var persistent *requestsqlite.Repository
	if cfg.Storage.Mode == config.StorageSQLite {
		persistent, err = openSQLite(requestsqlite.Options{
			Path: cfg.Storage.SQLitePath, RetentionDays: cfg.Storage.RetentionDays, Now: time.Now,
			OnWrite: func(id string, writeError error) {
				exchange, found := repository.Get(id)
				if !found {
					return
				}
				if writeError == nil {
					exchange.Persistence = &capture.PersistenceStatus{State: "persisted"}
				} else {
					exchange.Persistence = &capture.PersistenceStatus{State: "failed", Error: writeError.Error()}
				}
				repository.Save(exchange)
			},
			OnCleanup: func(cutoff time.Time, _ int64) {
				for _, removed := range repository.RemoveCompletedBefore(cutoff) {
					eventBus.Publish(events.Envelope{Type: events.ExchangeEvicted, ExchangeID: removed.ID, Revision: removed.Revision})
				}
			},
		})
		if err != nil {
			_ = managementListener.Close()
			_ = trafficListener.Close()
			if udpListener != nil {
				_ = udpListener.Close()
			}
			return nil, fmt.Errorf("initialize sqlite history: %w", err)
		}
	}
	history := store.NewRepository(repository, persistent)
	trafficHandler := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot {
			current, revision := configuration.Current()
			return capture.NewConfigSnapshot(current, revision)
		},
		Repository: history,
		Publisher:  eventBus,
		Handlers: map[string]capture.ModeHandler{
			config.ModeCapture: capture.CaptureHandler{},
			config.ModeProxy:   proxy.NewHandler(proxy.Options{Now: time.Now}),
		},
		ID:  capture.NewID,
		Now: time.Now,
	})
	traffic := newTrafficPlane(trafficHandler)
	traffic.Start(trafficListener, cfg)
	udp := newUDPPlane(udpPlaneOptions{Context: ctx, Configuration: configuration, History: history, Events: eventBus})
	udp.Start(udpListener, upstream, cfg.UDP)
	var management *managementPlane
	managementFactory := func(managementAddress string, shutdown <-chan struct{}) http.Handler {
		return httpapi.NewManagementHandler(httpapi.Options{
			Config: configuration, Repository: repository, Persistent: persistent, Events: eventBus,
			Build: build.Version, Commit: build.Commit, BuildDate: build.Date, Started: started, Static: staticHandler, Metrics: trafficHandler.Metrics, UDPStatus: udp.Status, Shutdown: shutdown, ManagementListener: managementAddress, TrafficListenerAddress: traffic.Address,
			PrepareManagementReload: func(next config.Config) (httpapi.ManagementReloadPlan, error) {
				return management.Prepare(next)
			},
			PrepareTrafficReload: func(next config.Config) (httpapi.TrafficReloadPlan, error) {
				return traffic.Prepare(next)
			},
			PrepareUDPReload: func(next config.Config) (httpapi.UDPReloadPlan, error) {
				return udp.Prepare(next)
			},
		})
	}
	management = newManagementPlane(managementFactory)
	management.Start(managementListener, cfg)
	runtime := &Runtime{
		ManagementAddress: management.Address(),
		TrafficAddress:    traffic.Address(),
		UDPAddress:        udp.Address(),
		done:              make(chan error, 1),
	}
	go serve(ctx, runtime.done, cfg.Shutdown, management, traffic, udp, persistent)
	return runtime, nil
}

func receiveUDP(listener *net.UDPConn, plane *udpPlane) {
	buffer := make([]byte, 65508)
	for {
		count, source, err := listener.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		udpConfig, sessions, active := plane.runtime(listener)
		if !active {
			continue
		}
		payload := append([]byte(nil), buffer[:min(count, udpConfig.MaxDatagramBytes)]...)
		current, revision := plane.configuration.Current()
		current.UDP = udpConfig
		now := time.Now()
		id, err := capture.NewID()
		if err != nil {
			continue
		}
		reservation, admitted := plane.history.Reserve(int64(len(payload)))
		preview := payload
		previewTruncated := int64(len(preview)) > current.Preview.Bytes
		if previewTruncated {
			preview = append([]byte(nil), preview[:current.Preview.Bytes]...)
		}
		exchange := capture.Exchange{ID: id, Transport: capture.TransportUDP, Mode: current.UDP.Mode, ConfigRevision: revision, Configuration: capture.NewConfigSnapshot(current, revision), Revision: 1, State: capture.StateCompleted, StartedAt: now, CompletedAt: now, Datagram: &capture.Datagram{SourceAddress: source.String(), LocalAddress: listener.LocalAddr().String(), AcceptedBytes: int64(len(payload)), Payload: payload, PayloadComplete: count <= current.UDP.MaxDatagramBytes, Preview: preview, PreviewTruncated: previewTruncated, Delivery: capture.DatagramDelivery{Result: "not_sent"}}}
		if count > current.UDP.MaxDatagramBytes {
			exchange.State = capture.StateDiscarded
			exchange.Error = &capture.ExchangeError{Category: capture.ErrorDatagramOversized, Message: "UDP datagram exceeds configured limit."}
			exchange.Datagram.DiscardReason = capture.ErrorDatagramOversized
		}
		if !admitted {
			exchange.State = capture.StateDiscarded
			exchange.Error = &capture.ExchangeError{Category: capture.ErrorDatagramOverload, Message: "UDP persistence capacity is exhausted."}
			exchange.Datagram.DiscardReason = capture.ErrorDatagramOverload
		}
		exchange.Duration = exchange.CompletedAt.Sub(exchange.StartedAt)
		if exchange.State != capture.StateDiscarded && current.UDP.Mode == config.ModeCapture && current.UDP.CaptureResponse != "" {
			reply := []byte(current.UDP.CaptureResponse)
			if len(reply) > len(payload) {
				reply, exchange.Datagram.Delivery.Truncated = reply[:len(payload)], true
			}
			sent, sendError := listener.WriteToUDP(reply, source)
			exchange.Datagram.Delivery.SentBytes = int64(sent)
			if sendError != nil {
				exchange.Datagram.Delivery.Result, exchange.Datagram.Delivery.Message = "failed", sendError.Error()
			} else if exchange.Datagram.Delivery.Truncated {
				exchange.Datagram.Delivery.Result, exchange.Datagram.Delivery.Message = "truncated", "Capture reply was limited to accepted datagram bytes."
			} else {
				exchange.Datagram.Delivery.Result = "sent"
			}
		}
		if exchange.State != capture.StateDiscarded && current.UDP.Mode == config.ModeProxy && sessions != nil {
			if forwardError := sessions.Forward(source, payload); forwardError != nil {
				exchange.Datagram.Delivery.Result, exchange.Datagram.Delivery.Message = "failed", forwardError.Error()
			} else {
				exchange.Datagram.Delivery.Result = "forwarded"
			}
		}
		if reservation != nil {
			exchange.Persistence = &capture.PersistenceStatus{State: "queued"}
		}
		plane.history.Save(exchange)
		if reservation != nil {
			if err := reservation.Commit(exchange); err != nil {
				exchange.Persistence = &capture.PersistenceStatus{State: "failed", Error: err.Error()}
				plane.history.Save(exchange)
			}
		}
		plane.events.Publish(events.Envelope{Type: events.RequestCompleted, ExchangeID: exchange.ID, Revision: exchange.Revision, Data: map[string]any{"transport": "udp", "datagram_bytes": len(payload)}})
	}
}

func (runtime *Runtime) Wait() error {
	return <-runtime.done
}

func serve(ctx context.Context, done chan<- error, shutdownTimeout time.Duration, management *managementPlane, traffic *trafficPlane, udp *udpPlane, persistent *requestsqlite.Repository) {
	var result error
	select {
	case <-ctx.Done():
	case err := <-traffic.Errors():
		result = err
	case err := <-management.Errors():
		result = err
	}
	management.CloseStreams()

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	targets := shutdownTargets{traffic: traffic, management: management}
	targets.udp = udp
	if persistent != nil {
		targets.persistence = persistent
	}
	shutdownError := shutdownRuntime(shutdownContext, targets)
	done <- errors.Join(result, shutdownError)
}

func shutdownRuntime(ctx context.Context, targets shutdownTargets) error {
	// Terminate ingestion immediately, then preserve every admitted SQLite write.
	trafficError := targets.traffic.Close()
	var udpError error
	if targets.udp != nil {
		udpError = targets.udp.Close()
	}
	var persistenceError error
	if targets.persistence != nil {
		// Persistence must finish admitted writes before the process can exit.
		persistenceError = targets.persistence.Close(context.Background())
	}
	managementError := targets.management.Shutdown(ctx)
	return errors.Join(trafficError, udpError, persistenceError, managementError)
}

func udpAddress(listener *net.UDPConn) string {
	if listener == nil {
		return ""
	}
	return listener.LocalAddr().String()
}

func startUDP(ctx context.Context, cfg config.UDP) (*net.UDPConn, netip.AddrPort, error) {
	if !cfg.Enabled {
		return nil, netip.AddrPort{}, nil
	}
	upstream, err := resolveUDPUpstream(ctx, cfg)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	listener, err := listenUDP(cfg.Listen)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	return listener, upstream, nil
}

func resolveUDPUpstream(ctx context.Context, cfg config.UDP) (netip.AddrPort, error) {
	if cfg.Mode != config.ModeProxy {
		return netip.AddrPort{}, nil
	}
	host, portText, err := net.SplitHostPort(cfg.Upstream)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("resolve UDP upstream: %w", err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return netip.AddrPort{}, fmt.Errorf("resolve UDP upstream: invalid port %q", portText)
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return checkedUDPUpstream(address, uint16(port))
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return netip.AddrPort{}, fmt.Errorf("resolve UDP upstream %q: %w", host, err)
	}
	for _, address := range addresses {
		if target, err := checkedUDPUpstream(address, uint16(port)); err != nil {
			return netip.AddrPort{}, err
		} else {
			return target, nil
		}
	}
	return netip.AddrPort{}, fmt.Errorf("resolve UDP upstream %q: no address", host)
}

func checkedUDPUpstream(address netip.Addr, port uint16) (netip.AddrPort, error) {
	if address.IsUnspecified() || address.IsMulticast() || address == netip.MustParseAddr("255.255.255.255") {
		return netip.AddrPort{}, errors.New("UDP upstream must resolve to unicast address")
	}
	return netip.AddrPortFrom(address.Unmap(), port), nil
}
