package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
)

type App struct {
	*state
	cfg Config
}

func New() *App {
	cfg := loadConfig()
	if cfg.MobileAccessToken == "" {
		cfg.MobileAccessToken = randomToken()
	}
	return &App{
		state: &state{
			clients:           make(map[net.Conn]bool),
			responseCollector: make(chan string, 1000),
			analysisQueue:     make(chan string, 1),
			mobileClients:     make(map[chan MobileEvent]struct{}),
		},
		cfg: cfg,
	}
}

func (a *App) Run() {
	go a.runAnalysisWorker()
	go a.startTCPServer()
	fmt.Println("HTTP Server listening on :8848")
	a.printMobileURLs()
	a.startHTTPServer()
}

func randomToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "change-me"
	}
	return hex.EncodeToString(b[:])
}
