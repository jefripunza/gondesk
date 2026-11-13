package database

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// AppSetting stores the local GonDesk profile and configuration in SQLite.
type AppSetting struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	DeskID            string    `gorm:"uniqueIndex;size:32;not null" json:"desk_id"`
	Alias             string    `gorm:"size:64;not null" json:"alias"`
	AccessPassword    string    `gorm:"size:128" json:"access_password"`
	UnattendedEnabled bool      `json:"unattended_enabled"`
	AllowControl      bool      `json:"allow_control"`
	AllowClipboard    bool      `json:"allow_clipboard"`
	BridgeServerURL   string    `gorm:"size:255" json:"bridge_server_url"`
	SinglePort        int       `json:"single_port"`
	EnableHTTPS       bool      `json:"enable_https"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// PeerRecord represents a discovered or saved GonDesk peer (Address Book / Recent Sessions / Online Bridge Registry).
type PeerRecord struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	DeskID          string    `gorm:"uniqueIndex;size:32;not null" json:"desk_id"`
	Alias           string    `gorm:"size:64" json:"alias"`
	Hostname        string    `gorm:"size:128" json:"hostname"`
	OS              string    `gorm:"size:32" json:"os"`
	EndpointURL     string    `gorm:"size:255" json:"endpoint_url"`
	Online          bool      `json:"online"`
	IsFavorite      bool      `json:"is_favorite"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	ConnectionCount int       `json:"connection_count"`
}

// SessionLog records remote desktop or bridge relay sessions in SQLite.
type SessionLog struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	SessionUUID   string     `gorm:"uniqueIndex;size:64;not null" json:"session_uuid"`
	ClientDeskID  string     `gorm:"index;size:32;not null" json:"client_desk_id"`
	HostDeskID    string     `gorm:"index;size:32;not null" json:"host_desk_id"`
	TransportMode string     `gorm:"size:64" json:"transport_mode"` // e.g., "HTTPS-WSS-SinglePort", "TURNS-SinglePort", "Direct-HTTPS"
	Status        string     `gorm:"size:32" json:"status"`         // "active", "completed", "rejected"
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	Notes         string     `gorm:"size:255" json:"notes"`
}

// TurnCredential stores TURN server authentication accounts in SQLite.
type TurnCredential struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Password  string    `gorm:"size:128;not null" json:"password"`
	Realm     string    `gorm:"size:64;not null" json:"realm"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// Store wraps the GORM SQLite database connection.
type Store struct {
	DB *gorm.DB
}

// Open initializes the SQLite database using GORM (pure-Go glebarez/sqlite driver)
// and runs schema migrations plus initial seed data.
func Open(dbPath string, defaultPort int, defaultBridgeURL string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.AutoMigrate(&AppSetting{}, &PeerRecord{}, &SessionLog{}, &TurnCredential{}); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	s := &Store{DB: db}
	if err := s.ensureDefaults(defaultPort, defaultBridgeURL); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureDefaults(defaultPort int, defaultBridgeURL string) error {
	var count int64
	if err := s.DB.Model(&AppSetting{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "gondesk-" + runtime.GOOS
		}
		setting := AppSetting{
			DeskID:            GenerateDeskID(),
			Alias:             hostname,
			AccessPassword:    "gondesk123",
			UnattendedEnabled: true,
			AllowControl:      true,
			AllowClipboard:    true,
			BridgeServerURL:   defaultBridgeURL,
			SinglePort:        defaultPort,
			EnableHTTPS:       true,
			UpdatedAt:         time.Now(),
		}
		if err := s.DB.Create(&setting).Error; err != nil {
			return err
		}
	}

	var turnCount int64
	if err := s.DB.Model(&TurnCredential{}).Count(&turnCount).Error; err != nil {
		return err
	}
	if turnCount == 0 {
		cred := TurnCredential{
			Username:  "gondesk",
			Password:  "gondesk-turn-secret",
			Realm:     "gondesk.local",
			Enabled:   true,
			CreatedAt: time.Now(),
		}
		_ = s.DB.Create(&cred).Error
	}

	var peerCount int64
	if err := s.DB.Model(&PeerRecord{}).Count(&peerCount).Error; err != nil {
		return err
	}
	if peerCount == 0 {
		now := time.Now()
		demos := []PeerRecord{
			{
				DeskID:          "482 910 374",
				Alias:           "Workstation-MacPro",
				Hostname:        "macpro-studio.local",
				OS:              "darwin/arm64",
				EndpointURL:     defaultBridgeURL,
				Online:          true,
				IsFavorite:      true,
				LastSeenAt:      now.Add(-2 * time.Minute),
				ConnectionCount: 5,
			},
			{
				DeskID:          "719 305 628",
				Alias:           "Office-Windows-PC",
				Hostname:        "win11-dev-rig",
				OS:              "windows/amd64",
				EndpointURL:     defaultBridgeURL,
				Online:          true,
				IsFavorite:      true,
				LastSeenAt:      now.Add(-5 * time.Minute),
				ConnectionCount: 3,
			},
			{
				DeskID:          "904 118 256",
				Alias:           "Ubuntu-Build-Node",
				Hostname:        "ubuntu-runner-01",
				OS:              "linux/amd64",
				EndpointURL:     defaultBridgeURL,
				Online:          false,
				IsFavorite:      false,
				LastSeenAt:      now.Add(-2 * time.Hour),
				ConnectionCount: 1,
			},
		}
		for _, p := range demos {
			_ = s.DB.Create(&p).Error
		}
	}

	return nil
}

// GetSettings returns the current AppSetting record.
func (s *Store) GetSettings() (AppSetting, error) {
	var st AppSetting
	err := s.DB.First(&st).Error
	return st, err
}

// SaveSettings updates the AppSetting record in SQLite.
func (s *Store) SaveSettings(st *AppSetting) error {
	st.UpdatedAt = time.Now()
	return s.DB.Save(st).Error
}

// ListPeers returns all saved/registered peers ordered by favorite and last seen.
func (s *Store) ListPeers() ([]PeerRecord, error) {
	var peers []PeerRecord
	err := s.DB.Order("is_favorite DESC, online DESC, last_seen_at DESC").Find(&peers).Error
	return peers, err
}

// UpsertPeer registers or updates a peer in SQLite.
func (s *Store) UpsertPeer(deskID, alias, hostname, osName, endpointURL string, online bool) (*PeerRecord, error) {
	deskID = NormalizeDeskID(deskID)
	var peer PeerRecord
	err := s.DB.Where("desk_id = ?", deskID).First(&peer).Error
	if err != nil {
		peer = PeerRecord{
			DeskID:      deskID,
			Alias:       alias,
			Hostname:    hostname,
			OS:          osName,
			EndpointURL: endpointURL,
			Online:      online,
			LastSeenAt:  time.Now(),
		}
		if alias == "" {
			peer.Alias = "Desk " + deskID
		}
		if err := s.DB.Create(&peer).Error; err != nil {
			return nil, err
		}
		return &peer, nil
	}

	if alias != "" {
		peer.Alias = alias
	}
	if hostname != "" {
		peer.Hostname = hostname
	}
	if osName != "" {
		peer.OS = osName
	}
	if endpointURL != "" {
		peer.EndpointURL = endpointURL
	}
	peer.Online = online
	peer.LastSeenAt = time.Now()
	if err := s.DB.Save(&peer).Error; err != nil {
		return nil, err
	}
	return &peer, nil
}

// ToggleFavorite toggles the favorite flag on a peer.
func (s *Store) ToggleFavorite(deskID string) error {
	deskID = NormalizeDeskID(deskID)
	var peer PeerRecord
	if err := s.DB.Where("desk_id = ?", deskID).First(&peer).Error; err != nil {
		return err
	}
	peer.IsFavorite = !peer.IsFavorite
	return s.DB.Save(&peer).Error
}

// IncrementPeerConnection bumps the connection count and LastSeenAt timestamp.
func (s *Store) IncrementPeerConnection(deskID, endpointURL string) {
	deskID = NormalizeDeskID(deskID)
	peer, err := s.UpsertPeer(deskID, "", "", "", endpointURL, true)
	if err == nil && peer != nil {
		peer.ConnectionCount++
		_ = s.DB.Save(peer).Error
	}
}

// RecordSession creates or updates a SessionLog entry in SQLite.
func (s *Store) RecordSession(uuid, clientID, hostID, transport, status, notes string) error {
	var logEntry SessionLog
	err := s.DB.Where("session_uuid = ?", uuid).First(&logEntry).Error
	if err != nil {
		logEntry = SessionLog{
			SessionUUID:   uuid,
			ClientDeskID:  NormalizeDeskID(clientID),
			HostDeskID:    NormalizeDeskID(hostID),
			TransportMode: transport,
			Status:        status,
			StartedAt:     time.Now(),
			Notes:         notes,
		}
		return s.DB.Create(&logEntry).Error
	}
	logEntry.Status = status
	if notes != "" {
		logEntry.Notes = notes
	}
	if status == "completed" || status == "rejected" || status == "disconnected" {
		now := time.Now()
		logEntry.EndedAt = &now
	}
	return s.DB.Save(&logEntry).Error
}

// ListSessionLogs returns recent session logs from SQLite.
func (s *Store) ListSessionLogs(limit int) ([]SessionLog, error) {
	if limit <= 0 {
		limit = 25
	}
	var logs []SessionLog
	err := s.DB.Order("started_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// ListTurnCredentials returns all TURN credentials stored in SQLite.
func (s *Store) ListTurnCredentials() ([]TurnCredential, error) {
	var creds []TurnCredential
	err := s.DB.Order("id ASC").Find(&creds).Error
	return creds, err
}

// LookupTurnPassword returns the password for an enabled TURN username.
func (s *Store) LookupTurnPassword(username string) (string, bool) {
	var cred TurnCredential
	if err := s.DB.Where("username = ? AND enabled = ?", username, true).First(&cred).Error; err != nil {
		return "", false
	}
	return cred.Password, true
}

// GenerateDeskID generates a 9-digit AnyDesk-style address (e.g. "583 920 417").
func GenerateDeskID() string {
	digits := make([]byte, 9)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			digits[i] = byte('1' + (i % 9))
			continue
		}
		if i == 0 && n.Int64() == 0 {
			digits[i] = '4'
		} else {
			digits[i] = byte('0' + n.Int64())
		}
	}
	return fmt.Sprintf("%s %s %s", string(digits[0:3]), string(digits[3:6]), string(digits[6:9]))
}

// NormalizeDeskID formats a raw 9-digit string into "XXX XXX XXX" if it consists of 9 digits.
func NormalizeDeskID(input string) string {
	trimmed := strings.TrimSpace(input)
	raw := strings.ReplaceAll(trimmed, " ", "")
	raw = strings.ReplaceAll(raw, "-", "")
	if len(raw) == 9 {
		allDigits := true
		for _, ch := range raw {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return fmt.Sprintf("%s %s %s", raw[0:3], raw[3:6], raw[6:9])
		}
	}
	return trimmed
}

