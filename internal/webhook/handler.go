package webhook

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/stepherg/blizzardgw/internal/events"
)

// IncomingEvent is a liberal structure for device events. Adjust as upstream schema firms up.
type IncomingEvent struct {
	Device  string          `json:"device"`
	Service string          `json:"service"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// Handler returns an http.HandlerFunc that ingests POSTed webhook events.
// TODO: Add signature validation (HMAC / JWT) once security model decided.
func Handler(bus *events.Bus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 512*1024))
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		// Content may be either JSON object or raw binary (e.g., USP). Try JSON first.
		var evt IncomingEvent
		if json.Unmarshal(body, &evt) == nil && evt.Device != "" && evt.Name != "" { // JSON form recognized
			bus.Publish(events.Event{Device: evt.Device, Service: evt.Service, Name: evt.Name, Payload: evt.Payload})
			// Debug log (structured-ish): JSON path
			log.Printf("webhook.debug ts=%s path=%s device=%s service=%s name=%s json=1 payload_bytes=%d payload_preview=%q", time.Now().Format(time.RFC3339Nano), r.URL.Path, evt.Device, nz(evt.Service, "BlizzardRDK"), evt.Name, len(evt.Payload), previewBytes(evt.Payload, 256))
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Fallback heuristic: attempt minimal parse from headers.
		device := r.Header.Get("X-Xmidt-Device")
		if device == "" {
			device = r.Header.Get("X-Device-ID")
		}
		name := r.Header.Get("X-Event-Name")
		if name == "" {
			name = "Unknown"
		}
		service := r.Header.Get("X-Service")
		if service == "" {
			service = "BlizzardRDK"
		}
		// Normalize
		device = strings.TrimSpace(device)
		bus.Publish(events.Event{Device: device, Service: service, Name: name, Payload: body})
		log.Printf("webhook.debug ts=%s path=%s device=%s service=%s name=%s json=0 payload_bytes=%d payload_preview=%q", time.Now().Format(time.RFC3339Nano), r.URL.Path, device, service, name, len(body), previewBytes(body, 256))
		w.WriteHeader(http.StatusAccepted)
	}
}

// previewBytes returns a printable (possibly truncated) string representation of raw bytes.
func previewBytes(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

// nz returns fallback if s is empty.
func nz(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
