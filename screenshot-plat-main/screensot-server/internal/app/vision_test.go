package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestParseQAHandlesModelFormatting(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		question   string
		answer     string
		structured bool
	}{
		{
			name:       "markdown fence",
			content:    "```json\n{\"question\":\"题干：测试\\nA. 一\",\"answer\":\"答案：A\\n解析：正确\"}\n```",
			question:   "题干：测试\nA. 一",
			answer:     "答案：A\n解析：正确",
			structured: true,
		},
		{
			name:       "double encoded JSON",
			content:    `"{\"question\":\"题干：测试\",\"answer\":\"答案：B\"}"`,
			question:   "题干：测试",
			answer:     "答案：B",
			structured: true,
		},
		{
			name:       "truncated JSON with raw newlines",
			content:    "```json\n{\"question\":\"题干：测试\nA. 一\nB. 二\",\"answer\":\"答案：A\n解析：正确",
			question:   "题干：测试\nA. 一\nB. 二",
			answer:     "答案：A\n解析：正确",
			structured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			question, answer, structured := parseQA(tt.content)
			if question != tt.question || answer != tt.answer || structured != tt.structured {
				t.Fatalf("parseQA() = (%q, %q, %v), want (%q, %q, %v)", question, answer, structured, tt.question, tt.answer, tt.structured)
			}
		})
	}
}

func TestCallVisionRetriesRateLimit(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, `{"code":50609,"message":"System is too busy now."}`, http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"question":"测试题","answer":"测试答案"}`}},
			},
		})
	}))
	defer server.Close()

	a := &App{cfg: Config{
		SiliconflowBaseURL:   server.URL,
		SiliconflowAPIKey:    "test-key",
		VisionTimeoutSeconds: 5,
		VisionMaxRetries:     2,
		VisionRetryDelayMS:   1,
	}}
	result := a.callVision(context.Background(), "test-model", "image")
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Question != "测试题" || result.Answer != "测试答案" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestCallVisionDoesNotRetryBadRequest(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, `{"message":"bad request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	a := &App{cfg: Config{
		SiliconflowBaseURL:   server.URL,
		SiliconflowAPIKey:    "test-key",
		VisionTimeoutSeconds: 5,
		VisionMaxRetries:     2,
		VisionRetryDelayMS:   1,
	}}
	result := a.callVision(context.Background(), "test-model", "image")
	if result.Error == "" {
		t.Fatal("expected an error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}

func TestCallVisionRetriesEmptySuccessfulResponse(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		content := ""
		if attempt > 1 {
			content = `{"question":"测试题","answer":"答案：正确"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"finish_reason": "stop",
					"message": map[string]string{
						"content": content,
					},
				},
			},
		})
	}))
	defer server.Close()

	a := &App{cfg: Config{
		SiliconflowBaseURL:   server.URL,
		SiliconflowAPIKey:    "test-key",
		VisionTimeoutSeconds: 5,
		VisionMaxRetries:     2,
		VisionRetryDelayMS:   1,
	}}
	result := a.callVision(context.Background(), "test-model", "image")
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Question != "测试题" || result.Answer != "答案：正确" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestCallVisionRejectsEmptyFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"finish_reason": "stop", "message": map[string]string{"content": `{"question":"","answer":""}`}},
			},
		})
	}))
	defer server.Close()

	a := &App{cfg: Config{
		SiliconflowBaseURL:   server.URL,
		SiliconflowAPIKey:    "test-key",
		VisionTimeoutSeconds: 5,
	}}
	result := a.callVision(context.Background(), "test-model", "image")
	if result.Error == "" {
		t.Fatal("expected an error for empty fields")
	}
}
