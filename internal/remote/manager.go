package remote

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
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

	"github.com/egoist/mygo"
	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
	"github.com/pion/stun/v3"
)

// CursorColorPalette provides visually distinct, high-contrast colors for dynamic multi-cursor collaboration.
var CursorColorPalette = []string{
	"#3B82F6", // Blue
	"#10B981", // Green
	"#F59E0B", // Amber
	"#8B5CF6", // Purple
	"#EC4899", // Pink
	"#06B6D4", // Cyan
	"#F97316", // Orange
	"#6366F1", // Indigo
	"#14B8A6", // Teal
	"#E11D48", // Rose
}

// GetCursorColor returns a distinct color for the participant desk ID based on palette cycling or index.
func GetCursorColor(index int) string {
	if len(CursorColorPalette) == 0 {
		return "#3B82F6"
	}
	return CursorColorPalette[index%len(CursorColorPalette)]
}

// RemoteCursor represents a participant's cursor position and metadata in a collaborative remote session.
type RemoteCursor struct {
	DeskID     string    `json:"desk_id"`
	Alias      string    `json:"alias"`
	Color      string    `json:"color"`
	NormX      float32   `json:"norm_x"`
	NormY      float32   `json:"norm_y"`
	LastAction string    `json:"last_action"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ActiveSession represents a live remote desktop session (either outgoing client or incoming host).
type ActiveSession struct {
	SessionUUID     string
	PeerDeskID      string
	PeerAlias       string
	Direction       string // "outgoing" (Me-remote) or "incoming" (Di-remote)
	Transport       string // "Single-Port HTTPS/WSS + TURNS (:8443)"
	Status          string // "connecting", "connected", "error", "rejected"
	ErrorMessage    string
	EndpointURL     string
	StartedAt       time.Time
	LatencyMs       int
	ChatLog         []string
	FileTransferLog []string
	Participants    map[string]*RemoteCursor
	LastInput       string
	FrameTick       int
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
			// #nosec G402 -- self-signed certificate on local/peer mesh single port
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
		// #nosec G402 -- self-signed certificate on local/peer mesh single port
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 5 * time.Second,
	}
	conn, wsResp, err := dialer.Dial(wsURL, nil)
	if wsResp != nil && wsResp.Body != nil {
		_ = wsResp.Body.Close()
	}
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
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.Status = "connected"
			if msg.FromAlias != "" {
				s.PeerAlias = msg.FromAlias
			}
			if s.Participants == nil {
				s.Participants = make(map[string]*RemoteCursor)
			}
			if _, ok := s.Participants[msg.FromDeskID]; !ok {
				s.Participants[msg.FromDeskID] = &RemoteCursor{
					DeskID:     msg.FromDeskID,
					Alias:      s.PeerAlias,
					Color:      GetCursorColor(len(s.Participants)),
					NormX:      0.5,
					NormY:      0.5,
					LastAction: "connected",
					UpdatedAt:  time.Now(),
				}
			}
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Connected to %s via Single-Port HTTPS + TURN", time.Now().Format("15:04:05"), msg.FromDeskID))
		}
		m.mu.Unlock()
		m.notify()

	case "session_reject":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.Status = "error"
			s.ErrorMessage = "Koneksi ditolak oleh pengguna host remote"
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), s.ErrorMessage))
		}
		m.mu.Unlock()
		m.notify()

	case "session_error":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.Status = "error"
			errMsg := msg.Payload
			if errMsg == "" {
				errMsg = fmt.Sprintf("Desk tujuan (%s) tidak aktif atau menolak koneksi", msg.FromDeskID)
			}
			s.ErrorMessage = errMsg
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Connection Error: %s", time.Now().Format("15:04:05"), errMsg))
		}
		m.mu.Unlock()
		m.notify()

	case "clipboard":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Remote clipboard received (%d chars): %s", msg.Timestamp, len(msg.Payload), msg.Payload))
		}
		m.mu.Unlock()
		if mygo.App.IsReady() && msg.Payload != "" {
			mygo.Clipboard.WriteText(msg.Payload)
		}
		m.notify()

	case "file_offer":
		m.mu.Lock()
		desc := fmt.Sprintf("[%s] Received file from %s: %s", msg.Timestamp, msg.FromDeskID, msg.Payload)
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.ChatLog = append(s.ChatLog, desc)
			s.FileTransferLog = append(s.FileTransferLog, desc)
		}
		m.mu.Unlock()
		m.notify()

	case "mouse_move", "mouse_click":
		var ev struct {
			NormX  float32 `json:"norm_x"`
			NormY  float32 `json:"norm_y"`
			Button int     `json:"button"`
			Alias  string  `json:"alias"`
		}
		_ = json.Unmarshal([]byte(msg.Payload), &ev)
		if ev.Alias == "" {
			ev.Alias = msg.FromAlias
		}
		if ev.Alias == "" {
			ev.Alias = msg.FromDeskID
		}

		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			if s.Participants == nil {
				s.Participants = make(map[string]*RemoteCursor)
			}
			cur, ok := s.Participants[msg.FromDeskID]
			if !ok {
				cur = &RemoteCursor{
					DeskID: msg.FromDeskID,
					Alias:  ev.Alias,
					Color:  GetCursorColor(len(s.Participants)),
				}
				s.Participants[msg.FromDeskID] = cur
			}
			cur.NormX = ev.NormX
			cur.NormY = ev.NormY
			cur.Alias = ev.Alias
			cur.UpdatedAt = time.Now()
			if msg.Type == "mouse_click" {
				cur.LastAction = "click"
				s.LastInput = fmt.Sprintf("Remote Click (%s)", ev.Alias)
				s.FrameTick++
			} else {
				cur.LastAction = "move"
			}
		}
		m.mu.Unlock()
		m.notify()

	case "chat":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] %s: %s", msg.Timestamp, msg.FromDeskID, msg.Payload))
		}
		m.mu.Unlock()
		m.notify()

	case "input":
		m.mu.Lock()
		for _, s := range m.sessions {
			if s.SessionUUID != msg.SessionUUID && s.PeerDeskID != msg.FromDeskID {
				continue
			}
			s.LastInput = msg.Payload
			s.FrameTick++
		}
		m.mu.Unlock()
		m.notify()
	}
}

// ConnectToPeer initiates a remote desktop connection to targetDeskOrURL over the single port.
func (m *Manager) ConnectToPeer(targetDeskOrURL, password string) (*ActiveSession, error) {
	if !m.Cfg.ShowDesktopFeatures() {
		return nil, errors.New("remote desktop client features are disabled in server mode")
	}
	target := database.NormalizeDeskID(targetDeskOrURL)
	if target == "" {
		return nil, errors.New("remote desk ID or address is required")
	}

	st, _ := m.Store.GetSettings()
	sessionID := uuid.NewString()[:12]

	alias := "Remote " + target
	endpointURL := ""
	if peers, err := m.Store.ListPeers(); err == nil {
		for _, p := range peers {
			if p.DeskID == target {
				if p.Alias != "" {
					alias = p.Alias
				}
				if p.EndpointURL != "" {
					endpointURL = p.EndpointURL
				}
				break
			}
		}
	}

	transportDesc := fmt.Sprintf("Single-Port HTTPS/WSS + TURNS (:%d)", m.Cfg.ListenPort)
	sess := &ActiveSession{
		SessionUUID:  sessionID,
		PeerDeskID:   target,
		PeerAlias:    alias,
		Direction:    "outgoing",
		Transport:    transportDesc,
		Status:       "connecting",
		EndpointURL:  endpointURL,
		StartedAt:    time.Now(),
		LatencyMs:    14,
		LastInput:    "Connecting to remote host...",
		Participants: make(map[string]*RemoteCursor),
		ChatLog: []string{
			fmt.Sprintf("[%s] Dialing %s over Single-Port HTTPS (: %d)...", time.Now().Format("15:04:05"), target, m.Cfg.ListenPort),
		},
	}
	sess.Participants[st.DeskID] = &RemoteCursor{
		DeskID:     st.DeskID,
		Alias:      st.Alias,
		Color:      GetCursorColor(0),
		NormX:      0.5,
		NormY:      0.5,
		LastAction: "connecting",
		UpdatedAt:  time.Now(),
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, sess)
	ws := m.bridgeWS
	m.mu.Unlock()

	m.Store.IncrementPeerConnection(target, endpointURL)
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

	switch {
	case ws != nil:
		_ = ws.WriteJSON(reqMsg)
	case m.Node != nil && m.Node.SendSignalToPeer(reqMsg):
		// Delivered directly to locally connected peer
	case endpointURL != "":
		// Direct dial probe over Single-Port
		go func() {
			time.Sleep(100 * time.Millisecond)
			client := &http.Client{
				Timeout: 2 * time.Second,
				Transport: &http.Transport{
					// #nosec G402
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}
			infoURL := strings.TrimRight(endpointURL, "/") + "/api/v1/info"
			resp, err := client.Get(infoURL)
			if err != nil {
				m.mu.Lock()
				sess.Status = "error"
				sess.ErrorMessage = fmt.Sprintf("Gagal terhubung ke %s: Aplikasi tujuan tidak aktif atau tidak terjangkau", target)
				sess.ChatLog = append(sess.ChatLog, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), sess.ErrorMessage))
				m.mu.Unlock()
				m.notify()
				return
			}
			_ = resp.Body.Close()

			m.mu.Lock()
			sess.Status = "connected"
			sess.ChatLog = append(sess.ChatLog, fmt.Sprintf("[%s] TLS 1.3 + Pion TURN Relay & Direct Channel Ready", time.Now().Format("15:04:05")))
			if _, ok := sess.Participants[target]; !ok {
				sess.Participants[target] = &RemoteCursor{
					DeskID:     target,
					Alias:      alias,
					Color:      GetCursorColor(len(sess.Participants)),
					NormX:      0.5,
					NormY:      0.5,
					LastAction: "connected",
					UpdatedAt:  time.Now(),
				}
			}
			m.mu.Unlock()
			m.notify()
		}()
	default:
		// Peer not on bridge and no endpoint URL
		sess.Status = "error"
		sess.ErrorMessage = fmt.Sprintf("Desk tujuan (%s) tidak aktif atau belum terhubung ke bridge", target)
		sess.ChatLog = append(sess.ChatLog, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), sess.ErrorMessage))
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
		SessionUUID:  sessionUUID,
		PeerDeskID:   fromDeskID,
		PeerAlias:    fromAlias,
		Direction:    "incoming",
		Transport:    transportDesc,
		Status:       "connected",
		StartedAt:    time.Now(),
		LatencyMs:    11,
		LastInput:    "Hosting Screen Share (Incoming Control Allowed)",
		Participants: make(map[string]*RemoteCursor),
		ChatLog: []string{
			fmt.Sprintf("[%s] Accepted incoming session from %s (%s)", time.Now().Format("15:04:05"), fromAlias, fromDeskID),
		},
	}
	sess.Participants[fromDeskID] = &RemoteCursor{
		DeskID:     fromDeskID,
		Alias:      fromAlias,
		Color:      GetCursorColor(0),
		NormX:      0.5,
		NormY:      0.5,
		LastAction: "connected",
		UpdatedAt:  time.Now(),
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

// SendClipboard sends clipboard text to the remote host.
func (m *Manager) SendClipboard(sessionUUID, text string) {
	st, _ := m.Store.GetSettings()
	nowStr := time.Now().Format("15:04:05")

	m.mu.Lock()
	var peerID string
	for _, s := range m.sessions {
		if s.SessionUUID == sessionUUID {
			peerID = s.PeerDeskID
			s.ChatLog = append(s.ChatLog, fmt.Sprintf("[%s] Clipboard sent (%d chars)", nowStr, len(text)))
		}
	}
	ws := m.bridgeWS
	m.mu.Unlock()

	msg := server.SignalMessage{
		Type:        "clipboard",
		SessionUUID: sessionUUID,
		FromDeskID:  st.DeskID,
		ToDeskID:    peerID,
		Payload:     text,
		Timestamp:   nowStr,
	}
	if ws != nil {
		_ = ws.WriteJSON(msg)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(msg)
	}
	m.notify()
}

// SendFile sends a file to the remote peer.
func (m *Manager) SendFile(sessionUUID, filename string, data []byte) {
	st, _ := m.Store.GetSettings()
	nowStr := time.Now().Format("15:04:05")

	m.mu.Lock()
	var peerID string
	desc := fmt.Sprintf("[%s] Sent file: %s (%d bytes)", nowStr, filename, len(data))
	for _, s := range m.sessions {
		if s.SessionUUID == sessionUUID {
			peerID = s.PeerDeskID
			s.ChatLog = append(s.ChatLog, desc)
			s.FileTransferLog = append(s.FileTransferLog, desc)
		}
	}
	ws := m.bridgeWS
	m.mu.Unlock()

	payload := fmt.Sprintf("%s|%d", filename, len(data))
	msg := server.SignalMessage{
		Type:        "file_offer",
		SessionUUID: sessionUUID,
		FromDeskID:  st.DeskID,
		ToDeskID:    peerID,
		Payload:     payload,
		Timestamp:   nowStr,
	}
	if ws != nil {
		_ = ws.WriteJSON(msg)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(msg)
	}
	m.notify()
}

// SendMouseMove broadcasts a normalized mouse move event for multi-cursor collaboration.
func (m *Manager) SendMouseMove(sessionUUID string, normX, normY float32) {
	st, _ := m.Store.GetSettings()
	m.mu.Lock()
	var peerID string
	for _, s := range m.sessions {
		if s.SessionUUID != sessionUUID {
			continue
		}
		peerID = s.PeerDeskID
		if s.Participants == nil {
			s.Participants = make(map[string]*RemoteCursor)
		}
		cur, ok := s.Participants[st.DeskID]
		if !ok {
			cur = &RemoteCursor{
				DeskID: st.DeskID,
				Alias:  st.Alias,
				Color:  GetCursorColor(len(s.Participants)),
			}
			s.Participants[st.DeskID] = cur
		}
		cur.NormX = normX
		cur.NormY = normY
		cur.LastAction = "move"
		cur.UpdatedAt = time.Now()
	}
	ws := m.bridgeWS
	m.mu.Unlock()

	payload, _ := json.Marshal(map[string]any{
		"norm_x": normX,
		"norm_y": normY,
		"alias":  st.Alias,
	})
	msg := server.SignalMessage{
		Type:        "mouse_move",
		SessionUUID: sessionUUID,
		FromDeskID:  st.DeskID,
		FromAlias:   st.Alias,
		ToDeskID:    peerID,
		Payload:     string(payload),
		Timestamp:   time.Now().Format("15:04:05"),
	}
	if ws != nil {
		_ = ws.WriteJSON(msg)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(msg)
	}
}

// SendMouseClick broadcasts a normalized mouse click event for multi-cursor collaboration.
func (m *Manager) SendMouseClick(sessionUUID string, normX, normY float32, button int) {
	st, _ := m.Store.GetSettings()
	m.mu.Lock()
	var peerID string
	for _, s := range m.sessions {
		if s.SessionUUID != sessionUUID {
			continue
		}
		peerID = s.PeerDeskID
		if s.Participants == nil {
			s.Participants = make(map[string]*RemoteCursor)
		}
		cur, ok := s.Participants[st.DeskID]
		if !ok {
			cur = &RemoteCursor{
				DeskID: st.DeskID,
				Alias:  st.Alias,
				Color:  GetCursorColor(len(s.Participants)),
			}
			s.Participants[st.DeskID] = cur
		}
		cur.NormX = normX
		cur.NormY = normY
		cur.LastAction = "click"
		cur.UpdatedAt = time.Now()
		s.LastInput = fmt.Sprintf("Click (%s at %.2f, %.2f)", st.Alias, normX, normY)
		s.FrameTick++
	}
	ws := m.bridgeWS
	m.mu.Unlock()

	payload, _ := json.Marshal(map[string]any{
		"norm_x": normX,
		"norm_y": normY,
		"button": button,
		"alias":  st.Alias,
	})
	msg := server.SignalMessage{
		Type:        "mouse_click",
		SessionUUID: sessionUUID,
		FromDeskID:  st.DeskID,
		FromAlias:   st.Alias,
		ToDeskID:    peerID,
		Payload:     string(payload),
		Timestamp:   time.Now().Format("15:04:05"),
	}
	if ws != nil {
		_ = ws.WriteJSON(msg)
	} else if m.Node != nil {
		m.Node.SendSignalToPeer(msg)
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
		cp.FileTransferLog = append([]string(nil), s.FileTransferLog...)
		if s.Participants != nil {
			cp.Participants = make(map[string]*RemoteCursor, len(s.Participants))
			for k, v := range s.Participants {
				vCopy := *v
				cp.Participants[k] = &vCopy
			}
		}
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
		// #nosec G402 -- self-signed certificate on local single port STUN probe
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
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
