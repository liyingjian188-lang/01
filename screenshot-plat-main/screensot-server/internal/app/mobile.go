package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// MobileEvent contains text-only data sent to phones. Screenshot data is never included.
type MobileEvent struct {
	ID        uint64 `json:"id"`
	Status    string `json:"status"`
	Model     string `json:"model,omitempty"`
	Question  string `json:"question,omitempty"`
	Answer    string `json:"answer,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (a *App) enqueueAnalysis(base64Image string) {
	select {
	case a.analysisQueue <- base64Image:
		a.broadcastMobile(MobileEvent{Status: "queued"})
	default:
		a.broadcastMobile(MobileEvent{Status: "busy", Error: "已有识别任务等待处理，请稍后再按 F8"})
	}
}

func (a *App) runAnalysisWorker() {
	for base64Image := range a.analysisQueue {
		a.broadcastMobile(MobileEvent{Status: "analyzing"})
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.visionTimeout())
		analyses := a.analyzeImages(ctx, []string{base64Image})
		cancel()
		a.setLastAnalyses(analyses)

		if len(analyses) == 0 || len(analyses[0].ModelAnswers) == 0 {
			a.broadcastMobile(MobileEvent{Status: "error", Error: "识别未返回结果"})
			continue
		}
		for _, answer := range analyses[0].ModelAnswers {
			if answer.Error == "" && strings.TrimSpace(answer.Question) == "" && strings.TrimSpace(answer.Answer) == "" {
				answer.Error = "AI 返回了空结果，请重新截图识别"
			}
			status := "complete"
			if answer.Error != "" {
				status = "error"
			}
			a.broadcastMobile(MobileEvent{
				Status:   status,
				Model:    answer.Model,
				Question: answer.Question,
				Answer:   answer.Answer,
				Error:    answer.Error,
			})
		}
	}
}

func (a *App) broadcastMobile(event MobileEvent) {
	event.ID = atomic.AddUint64(&a.eventSequence, 1)
	event.CreatedAt = time.Now().Format("2006-01-02 15:04:05")

	a.mobileMutex.Lock()
	copyOfEvent := event
	a.latestMobileEvent = &copyOfEvent
	for client := range a.mobileClients {
		select {
		case client <- event:
		default:
			select {
			case <-client:
			default:
			}
			select {
			case client <- event:
			default:
			}
		}
	}
	a.mobileMutex.Unlock()
}

func (a *App) subscribeMobile() (<-chan MobileEvent, func()) {
	client := make(chan MobileEvent, 8)
	a.mobileMutex.Lock()
	a.mobileClients[client] = struct{}{}
	if a.latestMobileEvent != nil {
		client <- *a.latestMobileEvent
	}
	a.mobileMutex.Unlock()

	return client, func() {
		a.mobileMutex.Lock()
		delete(a.mobileClients, client)
		a.mobileMutex.Unlock()
	}
}

func (a *App) handleMobile(w http.ResponseWriter, r *http.Request) {
	if !a.authorizedMobileRequest(r) {
		http.Error(w, "访问令牌无效，请使用服务端启动日志中的手机地址", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(mobileTemplate)
}

func (a *App) handleMobileCapture(w http.ResponseWriter, r *http.Request) {
	if !a.authorizedMobileRequest(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.sendAnalyzeCommandToClients() == 0 {
		http.Error(w, "电脑截图客户端未连接", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !a.authorizedMobileRequest(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	events, unsubscribe := a.subscribeMobile()
	defer unsubscribe()
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case event := <-events:
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.ID, payload)
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *App) authorizedMobileRequest(r *http.Request) bool {
	provided := r.URL.Query().Get("token")
	expected := a.cfg.MobileAccessToken
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (a *App) printMobileURLs() {
	token := url.QueryEscape(a.cfg.MobileAccessToken)
	fmt.Printf("Mobile page: http://127.0.0.1:8848/mobile?token=%s\n", token)

	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	seen := make(map[string]bool)
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip == nil || ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		value := ip.String()
		if seen[value] || strings.HasPrefix(value, "169.254.") {
			continue
		}
		seen[value] = true
		fmt.Printf("Mobile page: http://%s:8848/mobile?token=%s\n", value, token)
	}
}
