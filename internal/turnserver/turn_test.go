package turnserver

import (
	"net"
	"testing"
)

func TestTurnEngineStartAndAuth(t *testing.T) {
	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() { _ = udpConn.Close() }()

	tcpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer func() { _ = tcpLis.Close() }()

	lookup := func(username string) (string, bool) {
		if username == "testuser" {
			return "testsecret", true
		}
		return "", false
	}

	engine, err := Start("gondesk.local", "127.0.0.1", tcpLis, udpConn, lookup)
	if err != nil {
		t.Fatalf("Start turn engine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	if engine.AuthRequests() != 0 {
		t.Fatalf("expected initial auth requests 0, got %d", engine.AuthRequests())
	}
}

func TestTurnEngineNilSafeClose(t *testing.T) {
	var nilEngine *Engine
	if err := nilEngine.Close(); err != nil {
		t.Fatalf("expected nil engine Close to return nil, got %v", err)
	}
	if nilEngine.AuthRequests() != 0 {
		t.Fatalf("expected nil engine AuthRequests to return 0")
	}
}
