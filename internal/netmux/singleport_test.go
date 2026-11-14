package netmux

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestTLSConfigGenerationAndPersistence(t *testing.T) {
	// 1. In-memory generation
	tlsCfg, err := LoadOrGenerateTLSConfig("", "")
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig in-memory failed: %v", err)
	}
	if len(tlsCfg.Certificates) == 0 {
		t.Fatalf("expected generated certificate in tlsConfig")
	}

	// 2. Persistent file generation and reload
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	tlsCfgSaved, err := LoadOrGenerateTLSConfig(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig with paths failed: %v", err)
	}
	if len(tlsCfgSaved.Certificates) == 0 {
		t.Fatalf("expected saved certificate")
	}

	// Reload from existing files
	tlsCfgLoaded, err := LoadOrGenerateTLSConfig(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig reload failed: %v", err)
	}
	if len(tlsCfgLoaded.Certificates) == 0 {
		t.Fatalf("expected loaded certificate")
	}
}

func TestSinglePortMuxCreationAndRouting(t *testing.T) {
	tlsCfg, err := LoadOrGenerateTLSConfig("", "")
	if err != nil {
		t.Fatalf("TLS config failed: %v", err)
	}

	mux, err := NewSinglePortMux("127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("NewSinglePortMux failed: %v", err)
	}
	defer func() { _ = mux.Close() }()

	port := mux.Port()
	if port <= 0 {
		t.Fatalf("expected valid bound port > 0, got %d", port)
	}

	if mux.HTTPListener() == nil || mux.TURNListener() == nil || mux.UDPPacketConn() == nil {
		t.Fatalf("expected sub-listeners and UDP packet conn to be initialized")
	}

	go func() {
		_ = mux.Serve()
	}()

	// Start a simple HTTP handler on the mux HTTP listener
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "Single-Port HTTP OK")
		}),
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		_ = httpServer.Serve(mux.HTTPListener())
	}()
	defer func() { _ = httpServer.Close() }()

	// Connect a plain TCP client sending an HTTP GET request
	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", targetAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial single port failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_, err = fmt.Fprintf(conn, "GET /test HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", targetAddr)
	if err != nil {
		t.Fatalf("write HTTP request failed: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	foundResponse := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "Single-Port HTTP OK" || line == "HTTP/1.1 200 OK" {
			foundResponse = true
			break
		}
	}
	if !foundResponse {
		t.Fatalf("expected HTTP 200 or response body from single port listener")
	}

	// Verify stats
	httpConns, _, _ := mux.Stats()
	if httpConns < 1 {
		t.Fatalf("expected httpConns >= 1, got %d", httpConns)
	}
}
