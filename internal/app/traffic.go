package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/cors"
	"github.com/logocomune/requestinspector-relay/internal/httpapi"
)

type trafficPlane struct {
	mutex             sync.RWMutex
	transition        sync.Mutex
	handler           http.Handler
	listener          net.Listener
	server            *http.Server
	configuredAddress string
	serveErrors       chan error
	servers           map[*http.Server]struct{}
	corsEnabled       atomic.Bool
	waitGroup         sync.WaitGroup
	closed            bool
}

type trafficReloadPlan struct {
	plane     *trafficPlane
	config    config.Config
	listener  net.Listener
	result    httpapi.TrafficReload
	completed bool
}

func newTrafficPlane(handler http.Handler) *trafficPlane {
	plane := &trafficPlane{serveErrors: make(chan error, 1), servers: make(map[*http.Server]struct{})}
	plane.handler = cors.Handler(plane.corsEnabled.Load, handler)
	return plane
}

func (plane *trafficPlane) Start(listener net.Listener, cfg config.Config) {
	plane.corsEnabled.Store(cfg.CORS.Enabled)
	server := newTrafficServer(cfg, plane.handler)
	plane.mutex.Lock()
	plane.listener = listener
	plane.server = server
	plane.configuredAddress = cfg.Listeners.Traffic
	plane.servers[server] = struct{}{}
	plane.mutex.Unlock()
	plane.serve(server, listener)
}

func (plane *trafficPlane) Prepare(cfg config.Config) (httpapi.TrafficReloadPlan, error) {
	plane.transition.Lock()
	plane.mutex.RLock()
	if plane.closed {
		plane.mutex.RUnlock()
		plane.transition.Unlock()
		return nil, errors.New("traffic plane is closed")
	}
	configuredAddress := plane.configuredAddress
	currentAddress := plane.listener.Addr().String()
	plane.mutex.RUnlock()

	plan := &trafficReloadPlan{plane: plane, config: cfg}
	if cfg.Listeners.Traffic != configuredAddress {
		listener, err := net.Listen("tcp", cfg.Listeners.Traffic)
		if err != nil {
			log.Printf("traffic reload rejected: listener=%q error=%v", cfg.Listeners.Traffic, err)
			plane.transition.Unlock()
			return nil, fmt.Errorf("bind traffic listener %q: %w", cfg.Listeners.Traffic, err)
		}
		plan.listener = listener
		currentAddress = listener.Addr().String()
		plan.result.AddressChanged = true
	}
	plan.result.Listener = currentAddress
	return plan, nil
}

func (plan *trafficReloadPlan) Result() httpapi.TrafficReload {
	return plan.result
}

func (plan *trafficReloadPlan) Activate() {
	if plan.completed {
		return
	}
	plan.completed = true
	plane := plan.plane
	plane.corsEnabled.Store(plan.config.CORS.Enabled)
	if plan.listener == nil {
		plane.transition.Unlock()
		return
	}
	server := newTrafficServer(plan.config, plane.handler)
	plane.mutex.Lock()
	oldServer := plane.server
	plane.listener = plan.listener
	plane.server = server
	plane.configuredAddress = plan.config.Listeners.Traffic
	plane.servers[server] = struct{}{}
	plane.mutex.Unlock()
	plane.serve(server, plan.listener)
	plane.waitGroup.Add(1)
	go func() {
		defer plane.waitGroup.Done()
		drainTrafficServer(oldServer, plan.config.Shutdown)
		plane.mutex.Lock()
		delete(plane.servers, oldServer)
		plane.mutex.Unlock()
	}()
	log.Printf("traffic listener reloaded: listener=%q", plan.result.Listener)
	plane.transition.Unlock()
}

func (plan *trafficReloadPlan) Abort() {
	if plan.completed {
		return
	}
	plan.completed = true
	if plan.listener != nil {
		_ = plan.listener.Close()
	}
	plan.plane.transition.Unlock()
}

func (plane *trafficPlane) Address() string {
	plane.mutex.RLock()
	defer plane.mutex.RUnlock()
	if plane.listener == nil {
		return ""
	}
	return plane.listener.Addr().String()
}

func (plane *trafficPlane) Errors() <-chan error {
	return plane.serveErrors
}

func (plane *trafficPlane) Close() error {
	plane.transition.Lock()
	plane.mutex.Lock()
	plane.closed = true
	servers := make([]*http.Server, 0, len(plane.servers))
	for server := range plane.servers {
		servers = append(servers, server)
	}
	plane.mutex.Unlock()
	plane.transition.Unlock()
	closeErrors := make([]error, 0, len(servers))
	for _, server := range servers {
		closeErrors = append(closeErrors, server.Close())
	}
	plane.waitGroup.Wait()
	return errors.Join(closeErrors...)
}

func (plane *trafficPlane) serve(server *http.Server, listener net.Listener) {
	plane.waitGroup.Add(1)
	go func() {
		defer plane.waitGroup.Done()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = server.Close()
			select {
			case plane.serveErrors <- err:
			default:
			}
		}
	}()
}

func newTrafficServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:        handler,
		ReadTimeout:    cfg.Upstream.Timeout,
		WriteTimeout:   cfg.Upstream.Timeout,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: cfg.Limits.RequestHeaderBytes,
	}
}

func drainTrafficServer(server *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
}
