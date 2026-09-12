package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"screensot-server/internal/protocol"
	"testing"
)

func newMobileTestApp(token string) *App {
	return &App{
		state: &state{
			clients:       make(map[net.Conn]bool),
			mobileClients: make(map[chan MobileEvent]struct{}),
		},
		cfg: Config{MobileAccessToken: token},
	}
}

func TestHandleMobileCaptureRejectsInvalidToken(t *testing.T) {
	app := newMobileTestApp("secret")
	req := httptest.NewRequest(http.MethodPost, "/mobile/capture?token=wrong", nil)
	response := httptest.NewRecorder()

	app.handleMobileCapture(response, req)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestHandleMobileCaptureRequiresConnectedClient(t *testing.T) {
	app := newMobileTestApp("secret")
	req := httptest.NewRequest(http.MethodPost, "/mobile/capture?token=secret", nil)
	response := httptest.NewRecorder()

	app.handleMobileCapture(response, req)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleMobileCaptureSendsAnalyzeCommand(t *testing.T) {
	app := newMobileTestApp("secret")
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	app.clients[serverConn] = true

	command := make(chan string, 1)
	readError := make(chan error, 1)
	go func() {
		data, err := protocol.ReadWithLengthPrefix(clientConn)
		if err != nil {
			readError <- err
			return
		}
		command <- string(data)
	}()

	req := httptest.NewRequest(http.MethodPost, "/mobile/capture?token=secret", nil)
	response := httptest.NewRecorder()
	app.handleMobileCapture(response, req)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	select {
	case err := <-readError:
		t.Fatal(err)
	case got := <-command:
		if got != "2" {
			t.Fatalf("command = %q, want %q", got, "2")
		}
	}
}
