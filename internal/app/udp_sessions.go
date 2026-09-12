package app

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

type udpSessions struct {
	listener *net.UDPConn
	upstream netip.AddrPort
	config   config.UDP
	cache    *expirable.LRU[netip.AddrPort, *udpSession]
	onReject func(string)
	now      func() time.Time

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

type udpSession struct {
	connection *net.UDPConn
	client     netip.AddrPort
	manager    *udpSessions
	context    context.Context
	cancel     context.CancelFunc

	mu          sync.Mutex
	closed      bool
	windowStart time.Time
	replies     int
	bytes       int64
	forwarded   int64
	dropped     int64
}

func newUDPSessions(listener *net.UDPConn, upstream netip.AddrPort, cfg config.UDP) *udpSessions {
	manager := &udpSessions{listener: listener, upstream: upstream, config: cfg}
	manager.cache = expirable.NewLRU(cfg.MaxSessions, func(_ netip.AddrPort, session *udpSession) {
		session.close()
	}, cfg.SessionTTL)
	return manager
}

func (manager *udpSessions) Forward(source *net.UDPAddr, payload []byte) error {
	key, ok := canonicalUDPAddr(source)
	if !ok {
		return errors.New("invalid UDP source address")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return net.ErrClosed
	}
	session, found := manager.cache.Get(key)
	if !found {
		// Get does not evict an expired entry. Remove it so its socket is closed before replacement.
		manager.cache.Remove(key)
		connection, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(manager.upstream))
		if err != nil {
			return err
		}
		sessionContext, cancel := context.WithCancel(context.Background())
		session = &udpSession{connection: connection, client: key, manager: manager, context: sessionContext, cancel: cancel, windowStart: manager.currentTime()}
		manager.cache.Add(key, session)
		manager.wg.Add(1)
		go session.receive()
	} else {
		manager.cache.Add(key, session)
	}
	return session.forward(payload)
}

func (manager *udpSessions) Len() int {
	return manager.cache.Len()
}

func (manager *udpSessions) Close() error {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	manager.cache.Purge()
	manager.mu.Unlock()
	manager.wg.Wait()
	return nil
}

func (session *udpSession) forward(payload []byte) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return net.ErrClosed
	}
	_, err := session.connection.Write(payload)
	if err == nil {
		session.forwarded++
	}
	return err
}

func (session *udpSession) receive() {
	defer session.manager.wg.Done()
	buffer := make([]byte, session.manager.config.MaxDatagramBytes+1)
	for {
		count, err := session.connection.Read(buffer)
		if err != nil {
			return
		}
		if session.context.Err() != nil {
			return
		}
		if count > session.manager.config.MaxDatagramBytes {
			session.reject("reply_oversized")
			continue
		}
		if reason := session.allowReply(count); reason != "" {
			session.reject(reason)
			continue
		}
		_, _ = session.manager.listener.WriteToUDP(buffer[:count], net.UDPAddrFromAddrPort(session.client))
	}
}

func (session *udpSession) allowReply(size int) string {
	session.mu.Lock()
	defer session.mu.Unlock()
	now := session.manager.currentTime()
	if now.Sub(session.windowStart) >= time.Second {
		session.windowStart, session.replies, session.bytes = now, 0, 0
	}
	if session.replies >= session.manager.config.MaxRepliesPerSecond {
		return "reply_rate_limited"
	}
	if session.bytes+int64(size) > session.manager.config.MaxReplyBytesPerSecond {
		return "reply_byte_limited"
	}
	session.replies++
	session.bytes += int64(size)
	return ""
}

func (manager *udpSessions) currentTime() time.Time {
	if manager.now != nil {
		return manager.now()
	}
	return time.Now()
}

func (session *udpSession) reject(reason string) {
	session.mu.Lock()
	session.dropped++
	session.mu.Unlock()
	if session.manager.onReject != nil {
		session.manager.onReject(reason)
	}
}

func (session *udpSession) close() {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return
	}
	session.closed = true
	session.cancel()
	_ = session.connection.Close()
}

func canonicalUDPAddr(address *net.UDPAddr) (netip.AddrPort, bool) {
	if address == nil || address.Port < 1 || address.Port > 65535 {
		return netip.AddrPort{}, false
	}
	parsed, ok := netip.AddrFromSlice(address.IP)
	if !ok {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(parsed.Unmap(), uint16(address.Port)), true
}
