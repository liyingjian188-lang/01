package app

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"
)

func (a *App) startHTTPServer() {
	http.HandleFunc("/one", a.handleOne)
	http.HandleFunc("/mobile", a.handleMobile)
	http.HandleFunc("/mobile/capture", a.handleMobileCapture)
	http.HandleFunc("/events", a.handleEvents)
	if err := http.ListenAndServe(":8848", nil); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
	}
}

func (a *App) handleOne(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(
		os.Stderr,
		"handleOne: models=%v baseURL=%s timeout=%s keylen=%d tpl=%s\n",
		a.cfg.Models,
		a.cfg.SiliconflowBaseURL,
		a.cfg.visionTimeout(),
		len(a.cfg.SiliconflowAPIKey),
		a.cfg.TemplatePath,
	)

	mode := r.URL.Query().Get("mode")
	analyze := mode == "" || mode == "analyze"

	a.clientsMutex.Lock()
	numClients := len(a.clients)
	a.clientsMutex.Unlock()
	if numClients == 0 {
		http.Error(w, "No connected clients", http.StatusBadRequest)
		return
	}

	a.sendCaptureCommandToClients()

	var allResponses []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(numClients)
	for i := 0; i < numClients; i++ {
		go func() {
			defer wg.Done()
			select {
			case responseStr := <-a.responseCollector:
				mu.Lock()
				allResponses = append(allResponses, responseStr)
				mu.Unlock()
			case <-time.After(10 * time.Second):
				fmt.Println("Timeout waiting for client response")
			}
		}()
	}
	wg.Wait()

	var analyses []ImageEntry
	if analyze {
		ctx, cancel := context.WithTimeout(r.Context(), a.cfg.visionTimeout())
		defer cancel()
		analyses = a.analyzeImages(ctx, allResponses)
		a.setLastAnalyses(analyses)
	} else {
		last := a.getLastAnalyses()
		analyses = make([]ImageEntry, len(allResponses))
		for i := range allResponses {
			analyses[i] = ImageEntry{Base64: allResponses[i]}
			if i < len(last) && len(last[i].ModelAnswers) > 0 {
				analyses[i].ModelAnswers = append([]ModelAnswer(nil), last[i].ModelAnswers...)
			}
		}
	}

	type PageData struct{ Items []ImageEntry }
	data := PageData{Items: analyses}
	tplBytes, err := os.ReadFile(a.cfg.TemplatePath)
	if err != nil || len(tplBytes) == 0 {
		tplBytes = defaultTemplate
	}

	tmpl, err := template.New("result").Parse(string(tplBytes))
	if err != nil {
		http.Error(w, "Internal Server Error: unable to parse template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Internal Server Error: unable to execute template", http.StatusInternalServerError)
		return
	}
}
