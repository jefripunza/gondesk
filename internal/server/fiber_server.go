package server

import (
	"encoding/json"
	"net"
	"runtime"
	"sync"
	"time"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/netmux"
	"gondesk/internal/turnserver"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// SignalMessage is the real-time envelope sent over WebSocket (/ws/signal) on the single HTTPS port.
type SignalMessage struct {
	Type        string `json:"type"`                   // "register", "session_request", "session_accept", "session_reject", "chat", "input", "frame", "disconnect"
	SessionUUID string `json:"session_uuid,omitempty"` // unique session identifier
	FromDeskID  string `json:"from_desk_id"`
	FromAlias   string `json:"from_alias,omitempty"`
	ToDeskID    string `json:"to_desk_id"`
	Password    string `json:"password,omitempty"`
	Transport   string `json:"transport,omitempty"` // "Single-Port HTTPS/WSS + TURNS"
	Payload     string `json:"payload,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}

// LocalSignalHandler is invoked when a signaling message targets this local Desktop node.
type LocalSignalHandler func(msg SignalMessage)

// UnifiedNode coordinates the Single-Port Multiplexer (HTTPS GoFiber v2 + Pion TURN v4)
// and SQLite persistence for both Desktop and Desktop Server modes.
type UnifiedNode struct {
	Cfg          *config.Config
	Store        *database.Store
	Mux          *netmux.SinglePortMux
	TurnEngine   *turnserver.Engine
	FiberApp     *fiber.App
	localHandler LocalSignalHandler

	clientsMu sync.RWMutex
	clients   map[string]*wsPeerConn
}

type wsPeerConn struct {
	deskID string
	alias  string
	conn   *websocket.Conn
	writeM sync.Mutex
}

func (p *wsPeerConn) writeJSON(v any) error {
	p.writeM.Lock()
	defer p.writeM.Unlock()
	return p.conn.WriteJSON(v)
}

// NewUnifiedNode initializes the Single-Port Multiplexer, Pion TURN Server, and GoFiber v2 app.
func NewUnifiedNode(cfg *config.Config, store *database.Store, onLocalSignal LocalSignalHandler) (*UnifiedNode, error) {
	var tlsCfg = (*netmux.SinglePortMux)(nil)
	_ = tlsCfg

	tlsConfig, err := netmux.LoadOrGenerateTLSConfig(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	if !cfg.EnableTLS {
		tlsConfig = nil
	}

	mux, err := netmux.NewSinglePortMux(cfg.ListenAddr(), tlsConfig)
	if err != nil {
		return nil, err
	}

	// Update ListenPort if bound to :0 in tests
	cfg.ListenPort = mux.Port()

	turnEng, err := turnserver.Start(
		cfg.TurnRealm,
		cfg.PublicIP,
		mux.TURNListener(),
		mux.UDPPacketConn(),
		store.LookupTurnPassword,
	)
	if err != nil {
		_ = mux.Close()
		return nil, err
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		AppName:               "GonDesk Single-Port Bridge & Remote Engine",
	})

	node := &UnifiedNode{
		Cfg:          cfg,
		Store:        store,
		Mux:          mux,
		TurnEngine:   turnEng,
		FiberApp:     app,
		localHandler: onLocalSignal,
		clients:      make(map[string]*wsPeerConn),
	}

	node.registerRoutes()
	return node, nil
}

// Start launches the GoFiber listener and Single-Port Multiplexer in background goroutines.
func (n *UnifiedNode) Start() {
	go func() {
		_ = n.FiberApp.Listener(n.Mux.HTTPListener())
	}()
	go func() {
		_ = n.Mux.Serve()
	}()
}

// Stop cleanly shuts down GoFiber, Pion TURN, and the Single-Port Multiplexer.
func (n *UnifiedNode) Stop() error {
	_ = n.FiberApp.Shutdown()
	_ = n.TurnEngine.Close()
	return n.Mux.Close()
}

// OnlinePeerCount returns the number of currently connected WebSocket peers on this node.
func (n *UnifiedNode) OnlinePeerCount() int {
	n.clientsMu.RLock()
	defer n.clientsMu.RUnlock()
	return len(n.clients)
}

// SendSignalToPeer delivers a SignalMessage to a connected peer on this node.
func (n *UnifiedNode) SendSignalToPeer(msg SignalMessage) bool {
	target := database.NormalizeDeskID(msg.ToDeskID)
	n.clientsMu.RLock()
	peer, ok := n.clients[target]
	n.clientsMu.RUnlock()
	if !ok {
		return false
	}
	return peer.writeJSON(msg) == nil
}

func (n *UnifiedNode) registerRoutes() {
	api := n.FiberApp.Group("/api/v1")

	api.Get("/info", func(c *fiber.Ctx) error {
		st, _ := n.Store.GetSettings()
		httpConns, turnConns, tlsConns := n.Mux.Stats()
		return c.JSON(fiber.Map{
			"app":                  "gondesk",
			"mode":                 n.Cfg.Mode,
			"desktop_ui_enabled":   n.Cfg.ShowDesktopFeatures(),
			"desk_id":              st.DeskID,
			"alias":                st.Alias,
			"single_port":          n.Mux.Port(),
			"https_enabled":        n.Cfg.EnableTLS,
			"https_url":            n.Cfg.LocalHTTPSURL(),
			"turn_uri":             n.Cfg.TurnURI(),
			"turn_udp_port":        n.Mux.Port(),
			"online_ws_peers":      n.OnlinePeerCount(),
			"http_connections":     httpConns,
			"turn_connections":     turnConns,
			"tls_handshakes":       tlsConns,
			"turn_auth_requests":   n.TurnEngine.AuthRequests(),
		})
	})

	api.Get("/peers", func(c *fiber.Ctx) error {
		peers, err := n.Store.ListPeers()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"peers": peers,
		})
	})

	api.Post("/peers/heartbeat", func(c *fiber.Ctx) error {
		var req struct {
			DeskID      string `json:"desk_id"`
			Alias       string `json:"alias"`
			Hostname    string `json:"hostname"`
			OS          string `json:"os"`
			EndpointURL string `json:"endpoint_url"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if req.DeskID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "desk_id is required"})
		}
		if req.OS == "" {
			req.OS = runtime.GOOS + "/" + runtime.GOARCH
		}
		peer, err := n.Store.UpsertPeer(req.DeskID, req.Alias, req.Hostname, req.OS, req.EndpointURL, true)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"status":   "ok",
			"peer":     peer,
			"turn_uri": n.Cfg.TurnURI(),
		})
	})

	api.Get("/sessions", func(c *fiber.Ctx) error {
		logs, err := n.Store.ListSessionLogs(30)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"sessions": logs,
		})
	})

	api.Get("/turn/credentials", func(c *fiber.Ctx) error {
		creds, _ := n.Store.ListTurnCredentials()
		username := "gondesk"
		password := "gondesk-turn-secret"
		if len(creds) > 0 {
			username = creds[0].Username
			password = creds[0].Password
		}
		return c.JSON(fiber.Map{
			"realm":     n.Cfg.TurnRealm,
			"username":  username,
			"password":  password,
			"turns_uri": n.Cfg.TurnURI(),
			"turn_udp":  net.JoinHostPort(n.Cfg.PublicIP, strconvItoa(n.Mux.Port())),
			"port":      n.Mux.Port(),
		})
	})

	n.FiberApp.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	n.FiberApp.Get("/ws/signal", websocket.New(func(c *websocket.Conn) {
		deskID := database.NormalizeDeskID(c.Query("desk_id"))
		alias := c.Query("alias")
		if deskID == "" {
			deskID = database.GenerateDeskID()
		}

		peerConn := &wsPeerConn{
			deskID: deskID,
			alias:  alias,
			conn:   c,
		}

		n.clientsMu.Lock()
		n.clients[deskID] = peerConn
		n.clientsMu.Unlock()

		_, _ = n.Store.UpsertPeer(deskID, alias, "", runtime.GOOS+"/"+runtime.GOARCH, n.Cfg.LocalHTTPSURL(), true)

		defer func() {
			n.clientsMu.Lock()
			if current, ok := n.clients[deskID]; ok && current == peerConn {
				delete(n.clients, deskID)
			}
			n.clientsMu.Unlock()
			_, _ = n.Store.UpsertPeer(deskID, alias, "", "", "", false)
			_ = c.Close()
		}()

		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				break
			}
			var msg SignalMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			if msg.FromDeskID == "" {
				msg.FromDeskID = deskID
			} else {
				msg.FromDeskID = database.NormalizeDeskID(msg.FromDeskID)
			}
			msg.ToDeskID = database.NormalizeDeskID(msg.ToDeskID)
			if msg.Timestamp == "" {
				msg.Timestamp = time.Now().Format("15:04:05")
			}

			// Record session lifecycle events in SQLite
			switch msg.Type {
			case "session_request":
				_ = n.Store.RecordSession(msg.SessionUUID, msg.FromDeskID, msg.ToDeskID, "Single-Port HTTPS/WSS + TURNS", "requested", "Session requested via single-port bridge")
			case "session_accept":
				_ = n.Store.RecordSession(msg.SessionUUID, msg.FromDeskID, msg.ToDeskID, "Single-Port HTTPS/WSS + TURNS", "active", "Session accepted")
			case "session_reject":
				_ = n.Store.RecordSession(msg.SessionUUID, msg.FromDeskID, msg.ToDeskID, "Single-Port HTTPS/WSS + TURNS", "rejected", "Session rejected")
			case "disconnect":
				_ = n.Store.RecordSession(msg.SessionUUID, msg.FromDeskID, msg.ToDeskID, "Single-Port HTTPS/WSS + TURNS", "completed", "Session ended")
			}

			// Check if target is a connected WebSocket peer on this bridge
			if msg.ToDeskID != "" {
				if n.SendSignalToPeer(msg) {
					continue
				}
			}

			// Check if target is this local Desktop instance itself (Direct connection or loopback)
			localSettings, err := n.Store.GetSettings()
			if err == nil && (msg.ToDeskID == localSettings.DeskID || msg.ToDeskID == "local") {
				if n.Cfg.ShowDesktopFeatures() && n.localHandler != nil {
					n.localHandler(msg)
				}
			}
		}
	}))
}

func strconvItoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + (i % 10))
		i /= 10
	}
	return string(buf[pos:])
}

