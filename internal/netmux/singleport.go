package netmux

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// SinglePortMux multiplexes a single port number across:
//  1. HTTPS / HTTP / WebSocket (WSS/WS) -> routed to HTTPListener (GoFiber v2)
//  2. TURN / STUN over TLS (turns:) & TCP (turn:) -> routed to TURNListener (Pion TURN)
//  3. TURN / STUN over UDP -> bound on PacketConn on the exact same port number.
type SinglePortMux struct {
	rawTCP     net.Listener
	udpConn    net.PacketConn
	tlsConfig  *tls.Config
	httpLis    *chanListener
	turnLis    *chanListener
	closed     atomic.Bool
	closeOnce  sync.Once
	httpCount  atomic.Uint64
	turnCount  atomic.Uint64
	tlsCount   atomic.Uint64
}

// NewSinglePortMux binds both TCP and UDP on addr (e.g. ":8443" or "127.0.0.1:0")
// and prepares sub-listeners for GoFiber and Pion TURN.
func NewSinglePortMux(addr string, tlsConfig *tls.Config) (*SinglePortMux, error) {
	tcpLis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen tcp %s: %w", addr, err)
	}

	// Ensure UDP binds to the exact same port number (important when port 0 is used in tests).
	tcpAddr := tcpLis.Addr().(*net.TCPAddr)
	udpBindAddr := fmt.Sprintf("%s:%d", tcpAddr.IP.String(), tcpAddr.Port)
	if tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		udpBindAddr = fmt.Sprintf(":%d", tcpAddr.Port)
	}

	udpConn, err := net.ListenPacket("udp", udpBindAddr)
	if err != nil {
		_ = tcpLis.Close()
		return nil, fmt.Errorf("listen udp %s: %w", udpBindAddr, err)
	}

	m := &SinglePortMux{
		rawTCP:    tcpLis,
		udpConn:   udpConn,
		tlsConfig: tlsConfig,
		httpLis:   newChanListener(tcpLis.Addr()),
		turnLis:   newChanListener(tcpLis.Addr()),
	}
	return m, nil
}

// Port returns the actual bound TCP/UDP port number.
func (m *SinglePortMux) Port() int {
	if tcpAddr, ok := m.rawTCP.Addr().(*net.TCPAddr); ok {
		return tcpAddr.Port
	}
	return 0
}

// HTTPListener returns the net.Listener that receives decrypted HTTPS and plain HTTP/WS connections.
func (m *SinglePortMux) HTTPListener() net.Listener {
	return m.httpLis
}

// TURNListener returns the net.Listener that receives decrypted TURNS and plain TCP TURN/STUN connections.
func (m *SinglePortMux) TURNListener() net.Listener {
	return m.turnLis
}

// UDPPacketConn returns the UDP net.PacketConn bound to the same port number for Pion TURN UDP relay.
func (m *SinglePortMux) UDPPacketConn() net.PacketConn {
	return m.udpConn
}

// Stats returns counters of routed connections (HTTP/WSS, TURN TCP/TLS, TLS handshakes).
func (m *SinglePortMux) Stats() (httpConns, turnConns, tlsConns uint64) {
	return m.httpCount.Load(), m.turnCount.Load(), m.tlsCount.Load()
}

// Serve accepts incoming TCP connections and routes them to HTTPListener or TURNListener.
func (m *SinglePortMux) Serve() error {
	for {
		conn, err := m.rawTCP.Accept()
		if err != nil {
			if m.closed.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go m.handleConn(conn)
	}
}

func (m *SinglePortMux) handleConn(rawConn net.Conn) {
	_ = rawConn.SetReadDeadline(time.Now().Add(10 * time.Second))

	bc := newBufferedConn(rawConn)
	firstByte, err := bc.Peek(1)
	if err != nil {
		_ = rawConn.Close()
		return
	}

	var activeConn net.Conn = bc
	// TLS record type 0x16 = Handshake (ClientHello)
	if firstByte[0] == 0x16 && m.tlsConfig != nil {
		m.tlsCount.Add(1)
		tlsConn := tls.Server(bc, m.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			_ = rawConn.Close()
			return
		}
		activeConn = newBufferedConn(tlsConn)
	}

	_ = activeConn.SetReadDeadline(time.Time{})

	innerBC, ok := activeConn.(*bufferedConn)
	if !ok {
		innerBC = newBufferedConn(activeConn)
	}

	prefix, err := innerBC.Peek(4)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = innerBC.Close()
		return
	}

	if isHTTPMethodPrefix(prefix) {
		m.httpCount.Add(1)
		if !m.httpLis.enqueue(innerBC) {
			_ = innerBC.Close()
		}
		return
	}

	m.turnCount.Add(1)
	if !m.turnLis.enqueue(innerBC) {
		_ = innerBC.Close()
	}
}

func isHTTPMethodPrefix(b []byte) bool {
	if len(b) < 3 {
		return false
	}
	s := string(b)
	switch {
	case len(s) >= 4 && (s[:4] == "GET " || s[:4] == "POST" || s[:4] == "PUT " || s[:4] == "DELE" ||
		s[:4] == "HEAD" || s[:4] == "OPTI" || s[:4] == "PATC" || s[:4] == "CONN" || s[:4] == "PRI "):
		return true
	default:
		return false
	}
}

// Close shuts down the multiplexer and all sub-listeners.
func (m *SinglePortMux) Close() error {
	m.closeOnce.Do(func() {
		m.closed.Store(true)
		_ = m.httpLis.Close()
		_ = m.turnLis.Close()
		_ = m.rawTCP.Close()
		_ = m.udpConn.Close()
	})
	return nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func newBufferedConn(c net.Conn) *bufferedConn {
	return &bufferedConn{
		Conn: c,
		r:    bufio.NewReaderSize(c, 4096),
	}
}

func (b *bufferedConn) Peek(n int) ([]byte, error) {
	return b.r.Peek(n)
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

type chanListener struct {
	addr      net.Addr
	ch        chan net.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func newChanListener(addr net.Addr) *chanListener {
	return &chanListener{
		addr: addr,
		ch:   make(chan net.Conn, 256),
		done: make(chan struct{}),
	}
}

func (l *chanListener) enqueue(c net.Conn) bool {
	select {
	case <-l.done:
		return false
	case l.ch <- c:
		return true
	}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case c, ok := <-l.ch:
		if !ok {
			return nil, net.ErrClosed
		}
		return c, nil
	}
}

func (l *chanListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
	})
	return nil
}

func (l *chanListener) Addr() net.Addr {
	return l.addr
}

