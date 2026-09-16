package ui

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/skycontrol/skycontrol/internal/config"
)

//go:embed static/*
var staticFS embed.FS

type Connection struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
}

type Status struct {
	Callsign    string       `json:"callsign"`
	Airfield    string       `json:"airfield"`
	DistNM      float64      `json:"dist_nm"`
	OnGround    bool         `json:"on_ground"`
	Aircraft    int          `json:"aircraft"`
	LastATC     string       `json:"last_atc"`
	LastCS      string       `json:"last_callsign"`
	ATIS        string       `json:"atis"`
	Connections []Connection `json:"connections"`
}

type Hooks struct {
	Status  func() Status
	Command func(text string) string
	Log     *slog.Logger
}

func Start(ctx context.Context, addr string, h Hooks) {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.Status())
	})
	mux.HandleFunc("/api/command", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		atc := ""
		if h.Command != nil {
			atc = h.Command(body.Text)
		}
		writeJSON(w, map[string]string{"atc": atc})
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := config.Load()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, map[string]any{
				"schema": config.Schema(),
				"values": config.ValuesFrom(cfg),
			})
		case http.MethodPut, http.MethodPost:
			var body struct {
				Values map[string]any `json:"values"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if err := config.WriteYAML("config.yaml", body.Values); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "restart": true})
		default:
			http.Error(w, "GET or PUT", 405)
		}
	})
	mux.HandleFunc("/api/settings/defaults", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		if err := config.RestoreDefaults("config.yaml"); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "restart": true})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		path := "config.yaml"
		if r.Method == http.MethodPost {
			var body struct {
				YAML string `json:"yaml"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if err := os.WriteFile(path, []byte(body.YAML), 0644); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, map[string]string{"ok": "1"})
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(tailFile("skycontrol.log", 80)))
	})
	mux.HandleFunc("/api/open", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			What string `json:"what"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cwd, _ := os.Getwd()
		target := cwd
		switch body.What {
		case "config":
			target = filepath.Join(cwd, "config.yaml")
		case "log":
			target = filepath.Join(cwd, "skycontrol.log")
		}
		if err := openPath(target); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "1"})
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if h.Log != nil {
			h.Log.Warn("GUI server not started", "error", err, "addr", addr)
		}
		return
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if h.Log != nil {
		h.Log.Info("GUI ready", "url", "http://127.0.0.1"+addr)
	}
	go openBrowser("http://127.0.0.1" + addr)
	_ = srv.Serve(ln)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func openPath(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return exec.Command("explorer.exe", path).Start()
}

func openBrowser(url string) {
	time.Sleep(600 * time.Millisecond)
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("cmd", "/c", "start", "", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}