// Mock OpenAI 兼容服务器（chat + 视频异步），仅用于 E2E 测试。
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type videoTask struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"-"`
	PollCount  int       `json:"-"`
	Status     string    `json:"status"`
	Progress   float64   `json:"progress"`
	Videos     []video   `json:"videos,omitempty"`
}

type video struct {
	URL      string `json:"url"`
	Duration int    `json:"duration"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	MimeType string `json:"mime_type"`
}

var (
	tasksMu sync.Mutex
	tasks   = map[string]*videoTask{}
)

func main() {
	mux := http.NewServeMux()

	// chat completions（M2 用过的）
	mux.HandleFunc("/v1/chat/completions", handleChat)
	mux.HandleFunc("/v1/models", handleModels)

	// 视频异步生成
	mux.HandleFunc("/v1/videos/generations", handleVideoCreate)
	mux.HandleFunc("/v1/videos/generations/", handleVideoPoll)

	// 媒体下载
	mux.HandleFunc("/media/", handleMedia)

	addr := "127.0.0.1:19999"
	log.Printf("Mock server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func requireBearer(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, `{"error":{"message":"missing bearer"}}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireBearer(w, r) {
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	resp := map[string]any{
		"id":      "chatcmpl-mock-001",
		"object":  "chat.completion",
		"created": 1730000000,
		"model":   req["model"],
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello from mock!",
				},
				"finish_reason": "stop",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-mock","object":"model"},{"id":"sora-mock","object":"model"}]}`)
}

func handleVideoCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireBearer(w, r) {
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	id := fmt.Sprintf("vid-%s", randHex(8))
	t := &videoTask{
		ID:        id,
		CreatedAt: time.Now(),
		Status:    "queued",
		Progress:  0,
	}
	tasksMu.Lock()
	tasks[id] = t
	tasksMu.Unlock()

	log.Printf("MOCK videos/create id=%s body=%s", id, truncate(string(body), 100))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     id,
		"status": t.Status,
	})
}

// handleVideoPoll 模拟轮询：第 1 次 running 50%，第 2 次完成。
func handleVideoPoll(w http.ResponseWriter, r *http.Request) {
	if !requireBearer(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/videos/generations/")
	tasksMu.Lock()
	t, ok := tasks[id]
	if !ok {
		tasksMu.Unlock()
		http.Error(w, `{"error":{"message":"task not found"}}`, http.StatusNotFound)
		return
	}
	t.PollCount++
	switch {
	case t.PollCount < 2:
		t.Status = "running"
		t.Progress = 0.5
	default:
		t.Status = "completed"
		t.Progress = 1.0
		t.Videos = []video{
			{
				URL:      "http://127.0.0.1:19999/media/" + id + ".mp4",
				Duration: 5,
				Width:    1280,
				Height:   720,
				MimeType: "video/mp4",
			},
		}
	}
	out := *t
	tasksMu.Unlock()

	log.Printf("MOCK videos/poll id=%s poll_count=%d status=%s", id, t.PollCount, t.Status)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// handleMedia 返回一段假的 mp4 字节（这里用 1KB 随机数据模拟）。
// 真的 mp4 文件不需要——下游只是把字节落盘，不解析内容。
func handleMedia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", "1024")
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 1024)
	_, _ = rand.Read(buf)
	_, _ = w.Write(buf)
	log.Printf("MOCK media served path=%s", r.URL.Path)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	const hex = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, x := range b {
		out[i*2] = hex[x>>4]
		out[i*2+1] = hex[x&0xf]
	}
	return string(out)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
