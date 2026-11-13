package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/remote"
	"gondesk/internal/server"
	"gondesk/internal/uiapp"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	cfg := config.ParseFlags(os.Args[1:])

	store, err := database.Open(cfg.DBPath, cfg.ListenPort, cfg.BridgeServerURL)
	if err != nil {
		log.Fatalf("failed to initialize SQLite database: %v", err)
	}

	var mgr *remote.Manager
	onLocalSignal := func(msg server.SignalMessage) {
		if mgr != nil {
			mgr.HandleSignal(msg)
		}
	}

	node, err := startNodeWithFallback(cfg, store, onLocalSignal)
	if err != nil {
		log.Fatalf("failed to start single-port multiplexer: %v", err)
	}
	defer node.Stop()

	mgr = remote.NewManager(cfg, store, node, nil)
	appUI := uiapp.NewAppUI(cfg, store, node, mgr)
	mgr.OnChange = appUI.NotifyChanged

	log.Printf("[GonDesk] Mode=%s | DesktopFeatures=%v | SinglePort=:%d | HTTPS=%s | TURN=%s",
		cfg.Mode, cfg.ShowDesktopFeatures(), cfg.ListenPort, cfg.LocalHTTPSURL(), cfg.TurnURI())

	mgr.StartBridgeLink()

	if cfg.Headless {
		log.Printf("[GonDesk] Running in headless mode on %s (Press Ctrl+C to stop)", cfg.ListenAddr())
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		return
	}

	winTitle := fmt.Sprintf("GonDesk — Remote Desktop (1-Port HTTPS + TURN :%d)", cfg.ListenPort)
	if cfg.IsServerMode() {
		winTitle = fmt.Sprintf("GonDesk Server — Global Bridge (1-Port HTTPS + TURN :%d)", cfg.ListenPort)
	}

	mygo.App.WhenReady(func() {
		appUI.Win = mygo.NewWindow(mygo.WindowOptions{
			Title:     winTitle,
			Width:     1120,
			Height:    720,
			MinWidth:  860,
			MinHeight: 560,
			StateKey:  "gondesk-main-" + string(cfg.Mode),
			Content:   ui.View(appUI.View),
		})
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func startNodeWithFallback(cfg *config.Config, store *database.Store, onSignal server.LocalSignalHandler) (*server.UnifiedNode, error) {
	basePort := cfg.ListenPort
	for attempt := 0; attempt < 10; attempt++ {
		cfg.ListenPort = basePort + attempt
		node, err := server.NewUnifiedNode(cfg, store, onSignal)
		if err == nil {
			node.Start()
			return node, nil
		}
	}
	cfg.ListenPort = 0
	node, err := server.NewUnifiedNode(cfg, store, onSignal)
	if err != nil {
		return nil, err
	}
	node.Start()
	return node, nil
}

