package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/httpapi"
)

type managementHandlerFactory func(string, <-chan struct{}) http.Handler

type managementPlane struct {
	mutex             sync.RWMutex
	transition        sync.Mutex
	factory           managementHandlerFactory
	listener          net.Listener
	server            *http.Server
	handler           *reloadableHandler
	configuredAddress string
	streamShutdown    chan struct{}
	streamClosed      bool
	serveErrors       chan error
	servers           map[*http.Server]struct{}
	waitGroup         sync.WaitGroup
	closed            bool
}

type managementReloadPlan struct {
	plane     *managementPlane
	config    config.Config
	listener  net.Listener
	result    httpapi.ManagementReload
	completed bool
}

type reloadableHandler struct {
	mutex   sync.RWMutex
	handler http.Handler
}

func (handler *reloadableHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	handler.mutex.RLock()
	current := handler.handler
	handler.mutex.RUnlock()
	current.ServeHTTP(writer, request)
}

func (handler *reloadableHandler) Swap(next http.Handler) {
	handler.mutex.Lock()
	handler.handler = next
	handler.mutex.Unlock()
}

func newManagementPlane(factory managementHandlerFactory) *managementPlane {
	return &managementPlane{factory: factory, serveErrors: make(chan error, 1), servers: make(map[*http.Server]struct{})}
}

func (plane *managementPlane) Start(listener net.Listener, cfg config.Config) {
	shutdown := make(chan struct{})
	handler := &reloadableHandler{handler: plane.factory(listener.Addr().String(), shutdown)}
	server := newManagementServer(cfg, handler)
	plane.mutex.Lock()
	plane.listener = listener
	plane.server = server
	plane.handler = handler
	plane.configuredAddress = cfg.Listeners.Management
	plane.streamShutdown = shutdown
	plane.servers[server] = struct{}{}
	plane.mutex.Unlock()
	plane.serve(server, listener)
}

func (plane *managementPlane) Prepare(cfg config.Config) (httpapi.ManagementReloadPlan, error) {
	plane.transition.Lock()
	plane.mutex.RLock()
	if plane.closed {
		plane.mutex.RUnlock()
		plane.transition.Unlock()
		return nil, errors.New("management plane is closed")
	}
	configuredAddress := plane.configuredAddress
	currentAddress := plane.listener.Addr().String()
	plane.mutex.RUnlock()

	plan := &managementReloadPlan{plane: plane, config: cfg}
	if cfg.Listeners.Management != configuredAddress {
		listener, err := net.Listen("tcp", cfg.Listeners.Management)
		if err != nil {
			log.Printf("management reload rejected: listener=%q error=%v", cfg.Listeners.Management, err)
			plane.transition.Unlock()
			return nil, fmt.Errorf("bind management listener %q: %w", cfg.Listeners.Management, err)
		}
		plan.listener = listener
		currentAddress = listener.Addr().String()
		plan.result.AddressChanged = true
	}
	plan.result.Listener = currentAddress
	plan.result.ReloginRequired = cfg.Auth.Username != ""
	return plan, nil
}

func (plan *managementReloadPlan) Result() httpapi.ManagementReload {
	return plan.result
}

func (plan *managementReloadPlan) Activate() {
	if plan.completed {
		return
	}
	plan.completed = true
	plane := plan.plane
	shutdown := make(chan struct{})
	handler := plane.factory(plan.result.Listener, shutdown)

	plane.mutex.Lock()
	oldServer := plane.server
	oldShutdown := plane.streamShutdown
	oldStreamClosed := plane.streamClosed
	if plan.listener == nil {
		plane.handler.Swap(handler)
		plane.streamShutdown = shutdown
		plane.streamClosed = false
		plane.configuredAddress = plan.config.Listeners.Management
		plane.mutex.Unlock()
	} else {
		reloadable := &reloadableHandler{handler: handler}
		server := newManagementServer(plan.config, reloadable)
		plane.listener = plan.listener
		plane.server = server
		plane.handler = reloadable
		plane.configuredAddress = plan.config.Listeners.Management
		plane.streamShutdown = shutdown
		plane.streamClosed = false
		plane.servers[server] = struct{}{}
		plane.mutex.Unlock()
		plane.serve(server, plan.listener)
		plane.waitGroup.Add(1)
		go func() {
			defer plane.waitGroup.Done()
			drainManagementServer(oldServer, plan.config.Shutdown)
		}()
		log.Printf("management listener reloaded: listener=%q", plan.result.Listener)
	}
	if !oldStreamClosed {
		close(oldShutdown)
	}
	plane.transition.Unlock()
}

func (plan *managementReloadPlan) Abort() {
	if plan.completed {
		return
	}
	plan.completed = true
	if plan.listener != nil {
		_ = plan.listener.Close()
	}
	plan.plane.transition.Unlock()
}

func (plane *managementPlane) Address() string {
	plane.mutex.RLock()
	defer plane.mutex.RUnlock()
	if plane.listener == nil {
		return ""
	}
	return plane.listener.Addr().String()
}

func (plane *managementPlane) Errors() <-chan error {
	return plane.serveErrors
}

func (plane *managementPlane) CloseStreams() {
	plane.transition.Lock()
	plane.mutex.Lock()
	if !plane.streamClosed {
		close(plane.streamShutdown)
		plane.streamClosed = true
	}
	plane.mutex.Unlock()
	plane.transition.Unlock()
}

func (plane *managementPlane) Shutdown(ctx context.Context) error {
	plane.transition.Lock()
	plane.mutex.Lock()
	plane.closed = true
	if !plane.streamClosed {
		close(plane.streamShutdown)
		plane.streamClosed = true
	}
	servers := make([]*http.Server, 0, len(plane.servers))
	for server := range plane.servers {
		servers = append(servers, server)
	}
	plane.mutex.Unlock()
	plane.transition.Unlock()
	var shutdownErrors []error
	for _, server := range servers {
		shutdownErrors = append(shutdownErrors, server.Shutdown(ctx))
	}
	plane.waitGroup.Wait()
	return errors.Join(shutdownErrors...)
}

func (plane *managementPlane) serve(server *http.Server, listener net.Listener) {
	plane.waitGroup.Add(1)
	go func() {
		defer plane.waitGroup.Done()
		err := server.Serve(listener)
		plane.mutex.Lock()
		delete(plane.servers, server)
		plane.mutex.Unlock()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = server.Close()
			select {
			case plane.serveErrors <- err:
			default:
			}
		}
	}()
}

func newManagementServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:        handler,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: cfg.Limits.RequestHeaderBytes,
	}
}

func drainManagementServer(server *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
}
