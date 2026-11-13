package turnserver

import (
	"fmt"
	"net"
	"sync/atomic"

	"github.com/pion/turn/v4"
)

// PasswordLookupFunc resolves a TURN username to its plain password (stored in SQLite).
type PasswordLookupFunc func(username string) (password string, ok bool)

// Engine manages a Pion TURN v4 server attached to the single-port multiplexer.
type Engine struct {
	server       *turn.Server
	realm        string
	publicIP     net.IP
	authRequests atomic.Uint64
}

// Start creates and starts a Pion TURN server over the provided single-port TCP/TLS listener and UDP PacketConn.
func Start(realm, publicIPStr string, tcpListener net.Listener, udpConn net.PacketConn, lookup PasswordLookupFunc) (*Engine, error) {
	pubIP := net.ParseIP(publicIPStr)
	if pubIP == nil || pubIP.IsUnspecified() {
		pubIP = net.ParseIP("127.0.0.1")
	}

	eng := &Engine{
		realm:    realm,
		publicIP: pubIP,
	}

	relayGen := &turn.RelayAddressGeneratorStatic{
		RelayAddress: pubIP,
		Address:      "0.0.0.0",
	}

	var packetConns []turn.PacketConnConfig
	if udpConn != nil {
		packetConns = append(packetConns, turn.PacketConnConfig{
			PacketConn:            udpConn,
			RelayAddressGenerator: relayGen,
		})
	}

	var listeners []turn.ListenerConfig
	if tcpListener != nil {
		listeners = append(listeners, turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relayGen,
		})
	}

	srv, err := turn.NewServer(turn.ServerConfig{
		Realm: realm,
		AuthHandler: func(username, realmName string, srcAddr net.Addr) ([]byte, bool) {
			eng.authRequests.Add(1)
			if lookup != nil {
				if pwd, ok := lookup(username); ok {
					return turn.GenerateAuthKey(username, realmName, pwd), true
				}
			}
			// Fallback default account if lookup not matched
			if username == "gondesk" {
				return turn.GenerateAuthKey(username, realmName, "gondesk-turn-secret"), true
			}
			return nil, false
		},
		PacketConnConfigs: packetConns,
		ListenerConfigs:   listeners,
	})
	if err != nil {
		return nil, fmt.Errorf("create pion turn server: %w", err)
	}

	eng.server = srv
	return eng, nil
}

// AuthRequests returns the total number of TURN authentication requests processed.
func (e *Engine) AuthRequests() uint64 {
	if e == nil {
		return 0
	}
	return e.authRequests.Load()
}

// Close stops the Pion TURN server.
func (e *Engine) Close() error {
	if e == nil || e.server == nil {
		return nil
	}
	return e.server.Close()
}

