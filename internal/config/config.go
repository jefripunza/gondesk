package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AppMode defines whether GonDesk runs as a full Remote Desktop client/host
// or as a Global Bridge Server (where desktop remote/be-remoted features are hidden).
type AppMode string

const (
	ModeDesktop AppMode = "desktop"
	ModeServer  AppMode = "server"
)

// Config holds runtime configuration for both Desktop and Desktop Server modes.
type Config struct {
	Mode            AppMode `json:"mode"`
	Headless        bool    `json:"headless"`
	ListenHost      string  `json:"listen_host"`
	ListenPort      int     `json:"listen_port"`
	PublicIP        string  `json:"public_ip"`
	EnableTLS       bool    `json:"enable_tls"`
	BridgeServerURL string  `json:"bridge_server_url"`
	DataDir         string  `json:"data_dir"`
	DBPath          string  `json:"db_path"`
	CertFile        string  `json:"cert_file"`
	KeyFile         string  `json:"key_file"`
	TurnRealm       string  `json:"turn_realm"`
}

// Default returns the default configuration.
func Default() *Config {
	dataDir := "data"
	mode := ModeDesktop
	if envMode := strings.ToLower(strings.TrimSpace(os.Getenv("GONDESK_MODE"))); envMode == "server" || envMode == "desktop-server" {
		mode = ModeServer
	}

	port := 8443
	if envPort := os.Getenv("GONDESK_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	bridgeURL := os.Getenv("GONDESK_BRIDGE_URL")
	if bridgeURL == "" {
		bridgeURL = fmt.Sprintf("https://127.0.0.1:%d", port)
	}

	return &Config{
		Mode:            mode,
		Headless:        os.Getenv("GONDESK_HEADLESS") == "1" || os.Getenv("GONDESK_HEADLESS") == "true",
		ListenHost:      "0.0.0.0",
		ListenPort:      port,
		PublicIP:        "127.0.0.1",
		EnableTLS:       true,
		BridgeServerURL: bridgeURL,
		DataDir:         dataDir,
		DBPath:          filepath.Join(dataDir, "gondesk.db"),
		CertFile:        filepath.Join(dataDir, "certs", "server.crt"),
		KeyFile:         filepath.Join(dataDir, "certs", "server.key"),
		TurnRealm:       "gondesk.local",
	}
}

// ParseFlags parses command-line flags into a Config struct.
func ParseFlags(args []string) *Config {
	cfg := Default()
	fs := flag.NewFlagSet("gondesk", flag.ContinueOnError)

	modeStr := string(cfg.Mode)
	fs.StringVar(&modeStr, "mode", modeStr, "Application mode: 'desktop' (remote & be-remoted) or 'server' (global bridge server, desktop features hidden)")
	fs.BoolVar(&cfg.Headless, "headless", cfg.Headless, "Run without opening a GUI window (for server deployments)")
	fs.StringVar(&cfg.ListenHost, "host", cfg.ListenHost, "Host address for the single-port listener")
	fs.IntVar(&cfg.ListenPort, "port", cfg.ListenPort, "Single port for HTTPS (GoFiber), WSS Signaling/Relay, and Pion TURN/STUN")
	fs.StringVar(&cfg.PublicIP, "public-ip", cfg.PublicIP, "Public IP advertised by the Pion TURN relay")
	fs.BoolVar(&cfg.EnableTLS, "tls", cfg.EnableTLS, "Enable HTTPS and TURNS (TLS) on the single port")
	fs.StringVar(&cfg.BridgeServerURL, "bridge", cfg.BridgeServerURL, "Global Bridge Server HTTPS URL")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "Directory for SQLite database and TLS certificates")

	_ = fs.Parse(args)

	switch strings.ToLower(strings.TrimSpace(modeStr)) {
	case "server", "desktop-server", "bridge":
		cfg.Mode = ModeServer
	default:
		cfg.Mode = ModeDesktop
	}

	cfg.DBPath = filepath.Join(cfg.DataDir, "gondesk.db")
	cfg.CertFile = filepath.Join(cfg.DataDir, "certs", "server.crt")
	cfg.KeyFile = filepath.Join(cfg.DataDir, "certs", "server.key")
	return cfg
}

// IsServerMode returns true when running as the Global Bridge Server.
func (c *Config) IsServerMode() bool {
	return c.Mode == ModeServer
}

// ShowDesktopFeatures returns true only in Desktop mode.
// When in Desktop Server mode, remote/be-remoted desktop features are hidden.
func (c *Config) ShowDesktopFeatures() bool {
	return c.Mode == ModeDesktop
}

// ListenAddr returns the host:port string for the single-port listener.
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ListenHost, c.ListenPort)
}

// LocalHTTPSURL returns the local HTTPS/HTTP URL on the single port.
func (c *Config) LocalHTTPSURL() string {
	scheme := "http"
	if c.EnableTLS {
		scheme = "https"
	}
	host := c.PublicIP
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, c.ListenPort)
}

// TurnURI returns the TURN/TURNS URI on the single port.
func (c *Config) TurnURI() string {
	scheme := "turn"
	if c.EnableTLS {
		scheme = "turns"
	}
	host := c.PublicIP
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%s:%d?transport=tcp", scheme, host, c.ListenPort)
}

