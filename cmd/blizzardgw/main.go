package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stepherg/blizzardgw/internal/config"
	"github.com/stepherg/blizzardgw/internal/events"
	"github.com/stepherg/blizzardgw/internal/rpc"
	"github.com/stepherg/blizzardgw/internal/webhook"
	"github.com/stepherg/blizzardgw/internal/ws"
)

func main() {
	listen := flag.String("listen", ":8920", "listen address")
	flag.Parse()

	cfg := config.Default()
	cfg.Listen = *listen
	// Environment overrides (simple approach for now)
	if v := os.Getenv("SCYTALE_URL"); v != "" {
		cfg.ScytaleURL = v
	}
	if v := os.Getenv("SCYTALE_AUTH"); v != "" {
		cfg.ScytaleAuth = v
	}

	var dispatcher rpc.Dispatcher = rpc.EchoDispatcher{}
	if strings.TrimSpace(cfg.ScytaleURL) != "" {
		log.Printf("wrp bridging enabled -> %s", cfg.ScytaleURL)
		dispatcher = &rpc.WRPDispatcher{Client: &rpc.WRPClient{URL: cfg.ScytaleURL, Authorization: cfg.ScytaleAuth}, Source: "blizzard/gateway"}
	}

	// Event bus used for async event fanout
	bus := events.NewBus()

	// Webhook registration (raw Argus)
	// Apply defaults if not explicitly provided
	if os.Getenv("WEBHOOK_ENABLE") == "" {
		os.Setenv("WEBHOOK_ENABLE", "true")
	}
	if os.Getenv("ARGUS_URL") == "" {
		os.Setenv("ARGUS_URL", "http://argus:6600")
	}
	if os.Getenv("ARGUS_BASIC_AUTH") == "" {
		// Provided base64 user:pass (no Basic prefix) per instruction; add prefix if missing
		os.Setenv("ARGUS_BASIC_AUTH", "Basic dXNlcjpwYXNz")
	}
	if os.Getenv("ARGUS_BUCKET") == "" {
		os.Setenv("ARGUS_BUCKET", "hooks")
	}
	if os.Getenv("WEBHOOK_URL") == "" {
		// Gateway will listen on :8920; expose local endpoint path
		os.Setenv("WEBHOOK_URL", "http://blizzardgw:8920/webhook/events")
	}
	if os.Getenv("WEBHOOK_EVENTS") == "" {
		os.Setenv("WEBHOOK_EVENTS", ".*")
	}
	if os.Getenv("WEBHOOK_DEVICE_MATCH") == "" {
		os.Setenv("WEBHOOK_DEVICE_MATCH", ".*")
	}
	if os.Getenv("WEBHOOK_TTL") == "" {
		// 0 means let server default; choose explicit dev TTL (e.g., 86400 = 24h) for clarity
		os.Setenv("WEBHOOK_TTL", "86400")
	}

	if os.Getenv("WEBHOOK_ENABLE") == "true" {
		whCfg := webhook.Config{Enable: true,
			ArgusURL:    os.Getenv("ARGUS_URL"),
			Bucket:      os.Getenv("ARGUS_BUCKET"),
			AuthBasic:   os.Getenv("ARGUS_BASIC_AUTH"),
			CallbackURL: os.Getenv("WEBHOOK_URL"),
			TTL:         parseIntEnv("WEBHOOK_TTL", 0),
			Retries:     parseIntEnv("WEBHOOK_MAX_RETRIES", 3),
		}
		if ev := os.Getenv("WEBHOOK_EVENTS"); ev != "" {
			whCfg.Events = splitCSV(ev)
		}
		if dv := os.Getenv("WEBHOOK_DEVICE_MATCH"); dv != "" {
			whCfg.DeviceMatchers = splitCSV(dv)
		}
		// Prefer ancla-based registration; fallback to raw if dependencies unresolved.
		go func() {
			// Attempt ancla registration (will log if fails to init); context TODO: add cancel on shutdown.
			whCfg.RegisterAncla(context.Background())
		}()
		// Register ingestion endpoint
		http.HandleFunc("/webhook/events", webhook.Handler(bus))
	}

	h := &ws.Handler{
		Upgrader:    websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		Dispatcher:  dispatcher,
		SendBufSize: 64,
		Bus:         bus,
	}

	// Register both exact /ws and prefix /ws/ to allow clients to append /<device>/<service>
	http.Handle("/", h)
	http.Handle("/ws", h)
	log.Printf("blizzard gateway listening on %s", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, nil))
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if q := strings.TrimSpace(p); q != "" {
			out = append(out, q)
		}
	}
	return out
}

func parseIntEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

// ensure main doesn't exit immediately if webhook register needs brief time (optional small sleep for logs in ephemeral env)
func init() {
	time.Sleep(10 * time.Millisecond)
}
