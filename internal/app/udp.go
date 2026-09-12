package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync"

	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	"github.com/logocomune/requestinspector-relay/internal/httpapi"
	"github.com/logocomune/requestinspector-relay/internal/store"
)

type udpPlane struct {
	mutex         sync.RWMutex
	transition    sync.Mutex
	context       context.Context
	configuration *config.Manager
	history       *store.Repository
	events        *events.Bus
	config        config.UDP
	listener      *net.UDPConn
	sessions      *udpSessions
	waitGroup     sync.WaitGroup
	closed        bool
}

type udpPlaneOptions struct {
	Context       context.Context
	Configuration *config.Manager
	History       *store.Repository
	Events        *events.Bus
}

type udpReloadPlan struct {
	plane     *udpPlane
	config    config.UDP
	upstream  netip.AddrPort
	listener  *net.UDPConn
	result    httpapi.UDPReload
	completed bool
}

func newUDPPlane(options udpPlaneOptions) *udpPlane {
	return &udpPlane{context: options.Context, configuration: options.Configuration, history: options.History, events: options.Events}
}

func (plane *udpPlane) Start(listener *net.UDPConn, upstream netip.AddrPort, cfg config.UDP) {
	plane.mutex.Lock()
	plane.config = cfg
	plane.listener = listener
	plane.sessions = plane.newSessions(listener, upstream, cfg)
	plane.mutex.Unlock()
	if listener != nil {
		plane.receive(listener)
	}
}

func (plane *udpPlane) Prepare(cfg config.Config) (httpapi.UDPReloadPlan, error) {
	plane.transition.Lock()
	plane.mutex.RLock()
	if plane.closed {
		plane.mutex.RUnlock()
		plane.transition.Unlock()
		return nil, errors.New("UDP plane is closed")
	}
	currentConfig := plane.config
	currentListener := plane.listener
	plane.mutex.RUnlock()

	plan := &udpReloadPlan{plane: plane, config: cfg.UDP, result: httpapi.UDPReload{Enabled: cfg.UDP.Enabled}}
	if !cfg.UDP.Enabled {
		plan.result.AddressChanged = currentListener != nil
		return plan, nil
	}

	upstream, err := resolveUDPUpstream(plane.context, cfg.UDP)
	if err != nil {
		log.Printf("UDP reload rejected: listener=%q error=%v", cfg.UDP.Listen, err)
		plane.transition.Unlock()
		return nil, fmt.Errorf("prepare UDP upstream: %w", err)
	}
	plan.upstream = upstream
	if currentListener == nil || currentConfig.Listen != cfg.UDP.Listen {
		listener, bindError := listenUDP(cfg.UDP.Listen)
		if bindError != nil {
			log.Printf("UDP reload rejected: listener=%q error=%v", cfg.UDP.Listen, bindError)
			plane.transition.Unlock()
			return nil, bindError
		}
		plan.listener = listener
		plan.result.AddressChanged = true
		plan.result.Listener = listener.LocalAddr().String()
		return plan, nil
	}
	plan.result.Listener = currentListener.LocalAddr().String()
	return plan, nil
}

func (plan *udpReloadPlan) Result() httpapi.UDPReload {
	return plan.result
}

func (plan *udpReloadPlan) Activate() {
	if plan.completed {
		return
	}
	plan.completed = true
	plane := plan.plane
	plane.mutex.Lock()
	oldListener := plane.listener
	oldSessions := plane.sessions
	listener := oldListener
	if !plan.config.Enabled {
		listener = nil
	} else if plan.listener != nil {
		listener = plan.listener
	}
	plane.config = plan.config
	plane.listener = listener
	plane.sessions = plane.newSessions(listener, plan.upstream, plan.config)
	plane.mutex.Unlock()

	if plan.listener != nil {
		plane.receive(plan.listener)
	}
	if oldSessions != nil {
		_ = oldSessions.Close()
	}
	if oldListener != nil && oldListener != listener {
		_ = oldListener.Close()
	}
	log.Printf("UDP listener reloaded: enabled=%t listener=%q", plan.result.Enabled, plan.result.Listener)
	plane.transition.Unlock()
}

func (plan *udpReloadPlan) Abort() {
	if plan.completed {
		return
	}
	plan.completed = true
	if plan.listener != nil {
		_ = plan.listener.Close()
	}
	plan.plane.transition.Unlock()
}

func (plane *udpPlane) Address() string {
	plane.mutex.RLock()
	defer plane.mutex.RUnlock()
	return udpAddress(plane.listener)
}

func (plane *udpPlane) Status() httpapi.UDPStatus {
	plane.mutex.RLock()
	defer plane.mutex.RUnlock()
	status := httpapi.UDPStatus{Enabled: plane.listener != nil, Listener: udpAddress(plane.listener)}
	if plane.sessions != nil {
		status.Sessions = plane.sessions.Len()
	}
	return status
}

func (plane *udpPlane) runtime(listener *net.UDPConn) (config.UDP, *udpSessions, bool) {
	plane.mutex.RLock()
	defer plane.mutex.RUnlock()
	return plane.config, plane.sessions, !plane.closed && plane.listener == listener
}

func (plane *udpPlane) Close() error {
	plane.transition.Lock()
	plane.mutex.Lock()
	plane.closed = true
	listener := plane.listener
	sessions := plane.sessions
	plane.listener = nil
	plane.sessions = nil
	plane.mutex.Unlock()
	plane.transition.Unlock()
	var sessionError, listenerError error
	if sessions != nil {
		sessionError = sessions.Close()
	}
	if listener != nil {
		listenerError = listener.Close()
	}
	plane.waitGroup.Wait()
	return errors.Join(sessionError, listenerError)
}

func (plane *udpPlane) receive(listener *net.UDPConn) {
	plane.waitGroup.Add(1)
	go func() {
		defer plane.waitGroup.Done()
		receiveUDP(listener, plane)
	}()
}

func (plane *udpPlane) newSessions(listener *net.UDPConn, upstream netip.AddrPort, cfg config.UDP) *udpSessions {
	if listener == nil || cfg.Mode != config.ModeProxy {
		return nil
	}
	sessions := newUDPSessions(listener, upstream, cfg)
	sessions.onReject = func(reason string) {
		plane.events.Publish(events.Envelope{Type: events.ResponseCompleted, Data: map[string]any{"transport": "udp", "delivery_result": reason}})
	}
	return sessions
}

func listenUDP(address string) (*net.UDPConn, error) {
	resolved, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP listener: %w", err)
	}
	listener, err := net.ListenUDP("udp", resolved)
	if err != nil {
		return nil, fmt.Errorf("listen for UDP traffic: %w", err)
	}
	return listener, nil
}
