package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Mode != ModeDesktop {
		t.Fatalf("expected ModeDesktop, got %s", cfg.Mode)
	}
	if cfg.ListenPort != 8443 {
		t.Fatalf("expected default port 8443, got %d", cfg.ListenPort)
	}
	if !cfg.EnableTLS {
		t.Fatalf("expected EnableTLS to be true")
	}
	if cfg.Headless {
		t.Fatalf("expected Headless to be false in default desktop mode")
	}
	if !cfg.ShowDesktopFeatures() {
		t.Fatalf("expected ShowDesktopFeatures to be true for default desktop")
	}
	if cfg.IsServerMode() {
		t.Fatalf("expected IsServerMode to be false for default desktop")
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	t.Setenv("GONDESK_MODE", "server")
	t.Setenv("GONDESK_PORT", "9443")
	t.Setenv("GONDESK_HEADLESS", "true")
	t.Setenv("GONDESK_BRIDGE_URL", "https://relay.gondesk.com:9443")

	cfg := Default()
	if cfg.Mode != ModeServer {
		t.Fatalf("expected ModeServer from env, got %s", cfg.Mode)
	}
	if cfg.ListenPort != 9443 {
		t.Fatalf("expected port 9443 from env, got %d", cfg.ListenPort)
	}
	if !cfg.Headless {
		t.Fatalf("expected Headless to be true from env")
	}
	if cfg.BridgeServerURL != "https://relay.gondesk.com:9443" {
		t.Fatalf("expected BridgeServerURL from env, got %s", cfg.BridgeServerURL)
	}
	if !cfg.IsServerMode() {
		t.Fatalf("expected IsServerMode to be true")
	}
	if cfg.ShowDesktopFeatures() {
		t.Fatalf("expected ShowDesktopFeatures to be false in server mode")
	}

	// Test PORT fallback when GONDESK_PORT is unset
	_ = os.Unsetenv("GONDESK_PORT")
	t.Setenv("PORT", "7000")
	cfgPort := Default()
	if cfgPort.ListenPort != 7000 {
		t.Fatalf("expected fallback port 7000 from PORT, got %d", cfgPort.ListenPort)
	}
}

func TestParseFlags(t *testing.T) {
	args := []string{
		"-mode", "server",
		"-port", "8888",
		"-headless=true",
		"-host", "127.0.0.1",
		"-public-ip", "10.0.0.5",
		"-tls=false",
		"-bridge", "http://10.0.0.5:8888",
		"-data-dir", "/tmp/gondesk-test",
	}

	cfg := ParseFlags(args)
	if cfg.Mode != ModeServer {
		t.Fatalf("expected ModeServer, got %s", cfg.Mode)
	}
	if cfg.ListenPort != 8888 {
		t.Fatalf("expected port 8888, got %d", cfg.ListenPort)
	}
	if !cfg.Headless {
		t.Fatalf("expected Headless true")
	}
	if cfg.EnableTLS {
		t.Fatalf("expected EnableTLS false")
	}
	if cfg.ListenAddr() != "127.0.0.1:8888" {
		t.Fatalf("expected ListenAddr 127.0.0.1:8888, got %s", cfg.ListenAddr())
	}
	if cfg.LocalHTTPSURL() != "http://10.0.0.5:8888" {
		t.Fatalf("expected LocalHTTPSURL http://10.0.0.5:8888, got %s", cfg.LocalHTTPSURL())
	}
	if cfg.TurnURI() != "turn:10.0.0.5:8888?transport=tcp" {
		t.Fatalf("expected TurnURI turn:10.0.0.5:8888?transport=tcp, got %s", cfg.TurnURI())
	}

	// Test with TLS enabled
	cfg.EnableTLS = true
	cfg.PublicIP = "0.0.0.0"
	if cfg.LocalHTTPSURL() != "https://127.0.0.1:8888" {
		t.Fatalf("expected 0.0.0.0 to fallback to 127.0.0.1 in LocalHTTPSURL, got %s", cfg.LocalHTTPSURL())
	}
	if cfg.TurnURI() != "turns:127.0.0.1:8888?transport=tcp" {
		t.Fatalf("expected turns URI with 127.0.0.1 fallback, got %s", cfg.TurnURI())
	}
}
