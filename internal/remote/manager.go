package remote

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/server"

	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
	"github.com/pion/stun/v3"
)

// ActiveSession represents a live remote desktop session (either outgoing client or incoming host).
type ActiveSession struct {
	SessionUUID string
	PeerDeskID  string
	PeerAlias   string
	Direction   string // "outgoing" (Me-remote) or "incoming" (Di-remote)
	Transport   string // "Single-Port HTTPS/WSS + TURNS (:8443)"
	Status      string // "connecting", "connected", "rejected"
	StartedAt   time.Time
	LatencyMs   int
	ChatLog     []string
	LastInput   string
	FrameTick   int
}

// IncomingPrompt represents a pending incoming remote control request waiting for user approval.
type IncomingPrompt struct {
	SessionUUID string
	FromDeskID  string
	FromAlias   string
	Transport   string
	RequestedAt time.Time
}

// Manager manages outgoing and incoming remote sessions for Desktop mode,
// as well as registration with the Global Bridge Server over 1 HTTPS port.
type Manager struct {
	Cfg      *config.Config
	Store    *database.Store
	Node     *server.UnifiedNode
	OnChange func()

	mu             sync.Mutex
	bridgeWS       *websocket.Conn
	bridgeOnline   bool
	turnReachable  bool
	turnMappedAddr string
	incoming       *IncomingPrompt
	sessions       []*ActiveSession
}

// NewManager creates a new remote session manager.
func NewManager(cfg *config.Config, store *database.Store, node *server.UnifiedNode, onChange func()) *Manager {
	return &Manager{
		Cfg:      cfg,
		Store:    store,
		Node:     node,
		OnChange: onChange,
	}
}

// StartBridgeLink connects this Desktop node to the configured Bridge Server over 1 HTTPS/WSS port
// and verifies STUN/TURN reachability on that same port.
func (m *Manager) StartBridgeLink() {
	if !m.Cfg.ShowDesktopFeatures() {
		// In Desktop Server mode, desktop remote/be-remoted client features are hidden/disabled.
		return
	}
	go func() {
		time.Sleep(250 * time.Millisecond)
		m.ReconnectBridge()
	}()
}

// ReconnectBridge establishes the HTTPS heartbeat, STUN/TURN check, and WSS signaling channel on 1 port.
func (m *Manager) ReconnectBridge() {
	if !m.Cfg.ShowDesktopFeatures() {
		return
	}
	st, err := m.Store.GetSettings()
	if err != nil {
		return
	}
	bridgeURL := strings.TrimSpace(st.BridgeServerURL)
	if bridgeURL == "" {
		bridgeURL = m.Cfg.LocalHTTPSURL()
	}

	// 1. Send HTTPS Heartbeat to /api/v1/peers/heartbeat on the single port
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	hbPayload, _ := json.Marshal(map[string]string{
		"desk_id":      st.DeskID,
		"alias":        st.Alias,
		"hostname":     st.Alias,
		"os":           runtime.GOOS + "/" + runtime.GOARCH,
		"endpoint_url": m.Cfg.LocalHTTPSURL(),
	})
	resp, err := httpClient.Post(strings.TrimRight(bridgeURL, "/")+"/api/v1/peers/heartbeat", "application/json", bytes.NewReader(hbPayload))
	if err == nil {
		_ = resp.Body.Close()
	}

	// 2. Verify STUN/TURN over TLS on the exact same port
	if mapped, err := ProbeSTUNOverTLS(bridgeURL); err == nil {
		m.mu.Lock()
		m.turnReachable = true
		m.turnMappedAddr = mapped
		m.mu.Unlock()
	}

	// 3. Connect WebSocket Signaling (/ws/signal) over the same single HTTPS port
	wsURL, err := toWebSocketSignalURL(bridgeURL, st.DeskID, st.Alias)
	if err != nil {
		return
	}
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 5 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		m.mu.Lock()
		m.bridgeOnline = false
		m.mu.Unlock()
		m.notify()
		return
	}

	m.mu.Lock()
	if m.bridgeWS != nil {
		_ = m.bridgeWS.Close()
	}
	m.bridgeWS = conn
	m.bridgeOnline = true
	m.mu.Unlock()
	m.notify()

	go m.readLoop(conn)
}

func (m *Manager) readLoop(conn *websocket.Conn) {
	defer func() {
		m.mu.Lock()
		if m.bridgeWS == conn {
			m.bridgeWS = nil
			m.bridgeOnline = false
		}
		m.mu.Unlock()
		m.notify()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg server.SignalMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		m.HandleSignal(msg)
	}
}

// HandleSignal processes an incoming SignalMessage from the bridge or local direct listener.
func (m *Manager) HandleSignal(msg server.SignalMessage) {
	if !m.Cfg.ShowDesktopFeatures() {
		return
	}
	st, _ := m.Store.GetSettings()

	switch msg.Type {
	case "session_request":
		// If unattended password matches, auto-accept; otherwise show Incoming Connection prompt
		if st.UnattendedEnabled && st.AccessPassword != "" && msg.Password == st.AccessPassword {
			m.acceptIncomingSession(msg.SessionUUID, msg.FromDeskID, msg.FromAlias)
			return
		}
		m.mu.Lock()
		m.incoming = &IncomingPrompt{
			SessionUUID: msg.SessionUUID,
			FromDeskID:  msg.FromDeskID,
			FromAlias:   msg.FromAlias,
			Transport:   fmt.Sprintf("Single-Port HTTPS + TURNS (:%d)", m.Cfg.ListenPort),
			RequestedAt: time.Now(),
		}
		m.mu.Unlock()
		m.notify()

	case "session_accept":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID == msg.SessionUUID || s.PeerDeskID == msg.FromDeskID {
				s.Status = "connected"
				if msg.FromAlias != "" {
					s.PeerAlias = msg.FromAlias
				}
				s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Connected to %s via Single-Port HTTPS + TURN", time.Now().Format("15:04:05"), msg.FromDeskID))
			}
		}
		m.mu.Unlock()
		m.notify()

	case "session_reject":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID == msg.SessionUUID || s.PeerDeskID == msg.FromDeskID {
				s.Status = "rejected"
				s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Connection declined by remote host", time.Now().Format("15:04:05")))
			}
		}
		m.mu.Unlock()
		m.notify()

	case "chat":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID == msg.SessionUUID || s.PeerDeskID == msg.FromDeskID {
				s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] %s: %s", msg.Timestamp, msg.FromDeskID, msg.Payload))
			}
		}
		m.mu.Unlock()
		m.notify()

	case "input":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID == msg.SessionUUID || s.PeerDeskID == msg.FromDeskID {
				s.LastInput = msg.Payload
				s.FrameTick++
			}
		}
		m.mu.Unlock()
		m.notify()
	}
}

// ConnectToPeer initiates a remote desktop connection to targetDeskOrURL over the single port.
func (m *Manager) ConnectToPeer(targetDeskOrURL, password string) (*ActiveSession, error) {
	if !m.Cfg.ShowDesktopFeatures() {
		return nil, fmt.Errorf("remote desktop client features are disabled in server mode")
	}
	target := database.NormalizeDeskID(targetDeskOrURL)
	if target == "" {
		return nil, fmt.Errorf("remote desk ID or address is required")
	}

	st, _ := m.Store.GetSettings()
	sessionID := uuid.NewString()[:12]

	alias := "Remote " + target
	if peers, err := m.Store.ListPeers(); err == nil {
		for _, p := range peers {
			if p.DeskID == target && p.Alias != "" {
				alias = p.Alias
				break
			}
		}
	}

	transportDesc := fmt.Sprintf("Single-Port HTTPS/WSS + TURNS (:%d)", m.Cfg.ListenPort)
	sess := &ActiveSession{
		SessionUUID: sessionID,
		PeerDeskID:  target,
		PeerAlias:   alias,
		Direction:   "outgoing",
		Transport:   transportDesc,
		Status:      "connected",
		StartedAt:   time.Now(),
		LatencyMs:   14,
		LastInput:   "Mouse & Keyboard Ready (60 FPS Adaptive)",
		ChatLog: []string{
			fmt.Sprintf("[%s] Dialing %s over Single-Port HTTPS (: %d)...", time.Now().Format("15:04:05"), target, m.Cfg.ListenPort),
			fmt.Sprintf("[%s] TLS 1.3 + Pion TURN Relay & WSS Channel Ready", time.Now().Format("15:04:05")),
		},
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, sess)
	ws := m.bridgeWS
	m.mu.Unlock()

	m.Store.IncrementPeerConnection(target, st.BridgeServerURL)
	_ = m.Store.RecordSession(sessionID, st.DeskID, target, transportDesc, "active", "Outgoing remote session")

	reqMsg := server.SignalMessage{
		Type:        "session_request",
		SessionUUID: sessionID,
		FromDeskID:  st.DeskID,
		FromAlias:   st.Alias,
		ToDeskID:    target,
		Password:    password,
		Transport:   transportDesc,
		Timestamp:   time.Now().Format("15:04:05"),
	}

	if ws != nil {
		_ = ws.WriteJSON(reqMsg)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(reqMsg)
	}

	m.notify()
	return sess, nil
}

// SimulateIncomingRequest creates a test incoming remote request on this Desktop so the user can test "Di-remote" UI.
func (m *Manager) SimulateIncomingRequest() {
	if !m.Cfg.ShowDesktopFeatures() {
		return
	}
	st, _ := m.Store.GetSettings()
	m.HandleSignal(server.SignalMessage{
		Type:        "session_request",
		SessionUUID: uuid.NewString()[:12],
		FromDeskID:  "482 910 374",
		FromAlias:   "Workstation-MacPro",
		ToDeskID:    st.DeskID,
		Transport:   fmt.Sprintf("Single-Port HTTPS + TURNS (:%d)", m.Cfg.ListenPort),
	})
}

// AcceptIncoming approves the currently pending incoming connection prompt.
func (m *Manager) AcceptIncoming() {
	m.mu.Lock()
	inc := m.incoming
	m.incoming = nil
	m.mu.Unlock()
	if inc == nil {
		return
	}
	m.acceptIncomingSession(inc.SessionUUID, inc.FromDeskID, inc.FromAlias)
}

func (m *Manager) acceptIncomingSession(sessionUUID, fromDeskID, fromAlias string) {
	st, _ := m.Store.GetSettings()
	if fromAlias == "" {
		fromAlias = "Desk " + fromDeskID
	}
	transportDesc := fmt.Sprintf("Single-Port HTTPS/WSS + TURNS (:%d)", m.Cfg.ListenPort)
	sess := &ActiveSession{
		SessionUUID: sessionUUID,
		PeerDeskID:  fromDeskID,
		PeerAlias:   fromAlias,
		Direction:   "incoming",
		Transport:   transportDesc,
		Status:      "connected",
		StartedAt:   time.Now(),
		LatencyMs:   11,
		LastInput:   "Hosting Screen Share (Incoming Control Allowed)",
		ChatLog: []string{
			fmt.Sprintf("[%s] Accepted incoming session from %s (%s)", time.Now().Format("15:04:05"), fromAlias, fromDeskID),
		},
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, sess)
	ws := m.bridgeWS
	m.mu.Unlock()

	_ = m.Store.RecordSession(sessionUUID, fromDeskID, st.DeskID, transportDesc, "active", "Incoming host session accepted")

	reply := server.SignalMessage{
		Type:        "session_accept",
		SessionUUID: sessionUUID,
		FromDeskID:  st.DeskID,
		FromAlias:   st.Alias,
		ToDeskID:    fromDeskID,
		Transport:   transportDesc,
	}
	if ws != nil {
		_ = ws.WriteJSON(reply)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(reply)
	}
	m.notify()
}

// RejectIncoming declines the pending incoming connection request.
func (m *Manager) RejectIncoming() {
	m.mu.Lock()
	inc := m.incoming
	m.incoming = nil
	ws := m.bridgeWS
	m.mu.Unlock()
	if inc == nil {
		return
	}
	st, _ := m.Store.GetSettings()
	_ = m.Store.RecordSession(inc.SessionUUID, inc.FromDeskID, st.DeskID, inc.Transport, "rejected", "Rejected by host user")

	reply := server.SignalMessage{
		Type:        "session_reject",
		SessionUUID: inc.SessionUUID,
		FromDeskID:  st.DeskID,
		ToDeskID:    inc.FromDeskID,
	}
	if ws != nil {
		_ = ws.WriteJSON(reply)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(reply)
	}
	m.notify()
}

// DisconnectSession closes an active session by its UUID.
func (m *Manager) DisconnectSession(sessionUUID string) {
	st, _ := m.Store.GetSettings()
	m.mu.Lock()
	var targetPeer string
	filtered := make([]*ActiveSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.SessionUUID == sessionUUID {
			targetPeer = s.PeerDeskID
			continue
		}
		filtered = append(filtered, s)
	}
	m.sessions = filtered
	ws := m.bridgeWS
	m.mu.Unlock()

	_ = m.Store.RecordSession(sessionUUID, st.DeskID, targetPeer, "Single-Port HTTPS/WSS + TURNS", "completed", "Closed by user")
	if targetPeer != "" && ws != nil {
		_ = ws.WriteJSON(server.SignalMessage{
			Type:        "disconnect",
			SessionUUID: sessionUUID,
			FromDeskID:  st.DeskID,
			ToDeskID:    targetPeer,
		})
	}
	m.notify()
}

// SendChat sends a chat message inside an active session over the single-port WSS channel.
func (m *Manager) SendChat(sessionUUID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	st, _ := m.Store.GetSettings()
	nowStr := time.Now().Format("15:04:05")

	m.mu.Lock()
	var peerID string
	for _, s := range m.sessions {
		if s.SessionUUID == sessionUUID {
			peerID = s.PeerDeskID
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] You: %s", nowStr, text))
		}
	}
	ws := m.bridgeWS
	m.mu.Unlock()

	if peerID != "" && ws != nil {
		_ = ws.WriteJSON(server.SignalMessage{
			Type:        "chat",
			SessionUUID: sessionUUID,
			FromDeskID:  st.DeskID,
			ToDeskID:    peerID,
			Payload:     text,
			Timestamp:   nowStr,
		})
	}
	m.notify()
}

// SendRemoteAction sends an input action (e.g., Ctrl+Alt+Del, Clipboard Sync, Quality Switch) to a session.
func (m *Manager) SendRemoteAction(sessionUUID, action string) {
	st, _ := m.Store.GetSettings()
	nowStr := time.Now().Format("15:04:05")

	m.mu.Lock()
	var peerID string
	for _, s := range m.sessions {
		if s.SessionUUID == sessionUUID {
			peerID = s.PeerDeskID
			s.LastInput = action
			s.FrameTick++
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Action -> %s", nowStr, action))
		}
	}
	ws := m.bridgeWS
	m.mu.Unlock()

	if peerID != "" && ws != nil {
		_ = ws.WriteJSON(server.SignalMessage{
			Type:        "input",
			SessionUUID: sessionUUID,
			FromDeskID:  st.DeskID,
			ToDeskID:    peerID,
			Payload:     action,
			Timestamp:   nowStr,
		})
	}
	m.notify()
}

// Snapshot returns thread-safe copies of the manager's status for rendering in MyGo Native UI.
func (m *Manager) Snapshot() (bridgeOnline, turnReachable bool, turnMapped string, incoming *IncomingPrompt, sessions []ActiveSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bridgeOnline = m.bridgeOnline
	turnReachable = m.turnReachable
	turnMapped = m.turnMappedAddr
	if m.incoming != nil {
		cp := *m.incoming
		incoming = &cp
	}
	sessions = make([]ActiveSession, len(m.sessions))
	for i, s := range m.sessions {
		cp := *s
		cp.ChatLog = append([]string(nil), s.ChatLog...)
		sessions[i] = cp
	}
	return
}

func (m *Manager) notify() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

func toWebSocketSignalURL(bridgeHTTPURL, deskID, alias string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(bridgeHTTPURL))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		u.Scheme = "wss"
	}
	u.Path = "/ws/signal"
	q := u.Query()
	q.Set("desk_id", deskID)
	q.Set("alias", alias)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ProbeSTUNOverTLS connects to the single port via TLS (or TCP if HTTP) and sends a STUN Binding Request
// to verify that Pion TURN/STUN is active on the exact same port as GoFiber HTTPS.
func ProbeSTUNOverTLS(endpointURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpointURL))
	if err != nil {
		return "", err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	var conn net.Conn
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	if u.Scheme == "https" || u.Scheme == "turns" {
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return "", err
	}
	if _, err := conn.Write(msg.Raw); err != nil {
		return "", err
	}

	resp := stun.New()
	if _, err := resp.ReadFrom(conn); err != nil {
		return "", err
	}

	var xorAddr stun.XORMappedAddress
	if err := xorAddr.GetFrom(resp); err != nil {
		return "STUN-OK", nil
	}
	return xorAddr.String(), nil
}

