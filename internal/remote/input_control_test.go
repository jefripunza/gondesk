package remote

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"

	"gondesk/internal/server"
)

func TestInputControlEngine_MoveAndClick(t *testing.T) {
	screenEngine := NewScreenCaptureEngine()
	engine := NewInputControlEngine(screenEngine)

	var moveMu sync.Mutex
	var lastX, lastY float32
	var moveCalled bool

	var btnMu sync.Mutex
	var lastBtn int
	var lastDown bool
	var btnCalled bool

	engine.SetCustomHandlers(
		func(normX, normY float32) {
			moveMu.Lock()
			lastX = normX
			lastY = normY
			moveCalled = true
			moveMu.Unlock()
		},
		func(button int, down bool) {
			btnMu.Lock()
			lastBtn = button
			lastDown = down
			btnCalled = true
			btnMu.Unlock()
		},
	)

	// Test MoveMouse with out-of-bounds coordinates to verify clamping
	engine.MoveMouse(-0.5, 1.5)
	moveMu.Lock()
	if !moveCalled || lastX != 0.0 || lastY != 1.0 {
		t.Fatalf("expected clamped coords (0, 1), got (%f, %f)", lastX, lastY)
	}
	moveMu.Unlock()

	// Test MouseButton down and up
	engine.MouseButton(0.4, 0.6, 1, true)
	btnMu.Lock()
	if !btnCalled || lastBtn != 1 || !lastDown {
		t.Fatalf("expected button 1 down, got btn=%d down=%v", lastBtn, lastDown)
	}
	btnMu.Unlock()

	engine.MouseButton(0.4, 0.6, 1, false)
	btnMu.Lock()
	if lastBtn != 1 || lastDown {
		t.Fatalf("expected button 1 up, got btn=%d down=%v", lastBtn, lastDown)
	}
	btnMu.Unlock()
}

func TestSendX11WarpPointer_PacketStructure(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sendX11WarpPointer(clientConn, 0.5, 0.5, "MIT-MAGIC-COOKIE-1", []byte{1, 2, 3, 4})
	}()

	// 1. Read setup request from client
	setupReq := make([]byte, 12+20+4)
	n, err := serverConn.Read(setupReq)
	if err != nil || n < 12 {
		t.Fatalf("failed reading setup request: %v (n=%d)", err, n)
	}

	// 2. Reply with X11 setup success
	replyRest := make([]byte, 64)
	binary.LittleEndian.PutUint32(replyRest[32:36], 0x12345678)
	binary.LittleEndian.PutUint16(replyRest[52:54], 1920)
	binary.LittleEndian.PutUint16(replyRest[54:56], 1080)

	setupReply := make([]byte, 8+len(replyRest))
	setupReply[0] = 1 // Success
	// #nosec G115 -- test mock buffer length fits in uint16
	binary.LittleEndian.PutUint16(setupReply[6:8], uint16(len(replyRest)/4))
	copy(setupReply[8:], replyRest)

	if _, err := serverConn.Write(setupReply); err != nil {
		t.Fatalf("failed writing setup reply: %v", err)
	}

	// 3. Read WarpPointer request (24 bytes)
	warpReq := make([]byte, 24)
	if _, err := serverConn.Read(warpReq); err != nil {
		t.Fatalf("failed reading warp request: %v", err)
	}

	if warpReq[0] != 41 { // Opcode 41: WarpPointer
		t.Fatalf("expected opcode 41 for WarpPointer, got %d", warpReq[0])
	}
	rootWin := binary.LittleEndian.Uint32(warpReq[8:12])
	if rootWin != 0x12345678 {
		t.Fatalf("expected root window 0x12345678, got 0x%x", rootWin)
	}
	// #nosec G115 -- coordinate fits in int16
	dstX := int16(binary.LittleEndian.Uint16(warpReq[20:22]))
	// #nosec G115 -- coordinate fits in int16
	dstY := int16(binary.LittleEndian.Uint16(warpReq[22:24]))
	if dstX != 960 || dstY != 540 {
		t.Fatalf("expected warped coords (960, 540), got (%d, %d)", dstX, dstY)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error from sendX11WarpPointer: %v", err)
	}
}

func TestManager_SignalInputInjection(t *testing.T) {
	mgr, _ := setupTestManager(t)

	engine := mgr.InputEngine()
	if engine == nil {
		t.Fatalf("expected inputEngine to be initialized")
	}

	var moveCalled bool
	var movedX, movedY float32
	var btnCalled bool
	var btnDown bool

	engine.SetCustomHandlers(
		func(normX, normY float32) {
			moveCalled = true
			movedX = normX
			movedY = normY
		},
		func(button int, down bool) {
			btnCalled = true
			btnDown = down
		},
	)

	// Send mouse_move signal to manager
	mgr.HandleSignal(server.SignalMessage{
		Type:        "mouse_move",
		SessionUUID: "test-sess-1",
		FromDeskID:  "816 692 488",
		Payload:     `{"norm_x":0.75,"norm_y":0.25,"alias":"MacBook"}`,
	})

	if !moveCalled || movedX != 0.75 || movedY != 0.25 {
		t.Fatalf("expected mouse move (0.75, 0.25), got called=%v (%f, %f)", moveCalled, movedX, movedY)
	}

	// Send mouse_click signal to manager
	mgr.HandleSignal(server.SignalMessage{
		Type:        "mouse_click",
		SessionUUID: "test-sess-1",
		FromDeskID:  "816 692 488",
		Payload:     `{"norm_x":0.75,"norm_y":0.25,"button":0,"alias":"MacBook"}`,
	})

	if !btnCalled || !btnDown {
		t.Fatalf("expected button click down=true, got called=%v down=%v", btnCalled, btnDown)
	}

	// Send mouse_up signal to manager
	mgr.HandleSignal(server.SignalMessage{
		Type:        "mouse_up",
		SessionUUID: "test-sess-1",
		FromDeskID:  "816 692 488",
		Payload:     `{"norm_x":0.75,"norm_y":0.25,"button":0,"alias":"MacBook"}`,
	})

	if !btnCalled || btnDown {
		t.Fatalf("expected button up down=false, got called=%v down=%v", btnCalled, btnDown)
	}
}
