package webhook

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	wrp "github.com/xmidt-org/wrp-go/v3"

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

		// Check Content-Type to determine if this is a WRP msgpack message
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "msgpack") && len(body) > 0 {
			// Decode WRP message
			dec := wrp.NewDecoder(bytes.NewReader(body), wrp.Msgpack)
			var msg wrp.Message
			if err := dec.Decode(&msg); err == nil {
				// Extract device ID from WRP source (format: "mac:xxxx/service")
				device := extractDeviceFromSource(msg.Source)
				service := extractServiceFromSource(msg.Source)
				if service == "" {
					service = "BlizzardRDK"
				}

				// Extract event name from destination (format: "event:Service/EventName")
				eventName := extractEventFromDestination(msg.Destination)
				if eventName == "" {
					eventName = "Unknown"
				}

				// The payload contains the actual JSON-RPC message
				// Publish it as-is (it's already JSON)
				bus.Publish(events.Event{
					Device:  device,
					Service: service,
					Name:    eventName,
					Payload: msg.Payload,
				})

				log.Printf("webhook.debug ts=%s path=%s device=%s service=%s name=%s wrp=1 payload_bytes=%d payload_preview=%q",
					time.Now().Format(time.RFC3339Nano), r.URL.Path, device, service, eventName,
					len(msg.Payload), previewBytes(msg.Payload, 256))
				w.WriteHeader(http.StatusAccepted)
				return
			}
			// If WRP decode fails, fall through to other parsing methods
			log.Printf("webhook.debug ts=%s path=%s wrp_decode_error=%v", time.Now().Format(time.RFC3339Nano), r.URL.Path, err)
		}

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

// extractDeviceFromSource extracts the device ID from WRP source field.
// Source format: "mac:112233445566/service" or "mac:112233445566"
func extractDeviceFromSource(source string) string {
	if source == "" {
		return ""
	}
	// Split on '/' to separate device from service
	parts := strings.Split(source, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return source
}

// extractServiceFromSource extracts the service name from WRP source field.
// Source format: "mac:112233445566/service" or "mac:112233445566"
func extractServiceFromSource(source string) string {
	parts := strings.Split(source, "/")
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

// extractEventFromDestination extracts the event name from WRP destination field.
// Destination format: "event:Service/EventName" or "event:Blizzard/Time/TimerElapsed"
func extractEventFromDestination(dest string) string {
	if dest == "" {
		return ""
	}
	// Remove "event:" prefix if present
	dest = strings.TrimPrefix(dest, "event:")

	// Split on '/' and take the last part as the event name
	parts := strings.Split(dest, "/")
	if len(parts) > 0 {
		// Join all parts after the first one (service name) with '.'
		// e.g., "Blizzard/Time/TimerElapsed" -> "Time.TimerElapsed"
		if len(parts) > 1 {
			return strings.Join(parts[1:], ".")
		}
		return parts[0]
	}
	return dest
}
