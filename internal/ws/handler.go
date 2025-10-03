package ws

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stepherg/blizzardgw/internal/events"
	"github.com/stepherg/blizzardgw/internal/rpc"
)

// Handler upgrades HTTP to WebSocket and processes JSON-RPC messages.
type Handler struct {
	Upgrader    websocket.Upgrader
	Dispatcher  rpc.Dispatcher // base dispatcher (used when path has no device/service)
	SendBufSize int
	Bus         *events.Bus // optional event bus; if nil notifications only synthetic
}

type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// Tunable timing constants (aligned with gorilla/websocket chat example pattern)
const (
	pongWait   = 75 * time.Second
	pingPeriod = (pongWait * 9) / 10
	writeWait  = 10 * time.Second
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade failed: %v", err)
		return
	}
	// Derive device/service from path: /ws/<device>/<service>
	// Trim leading / and split
	path := r.URL.Path
	// Expect base path starts with /ws
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var dispatcher rpc.Dispatcher = h.Dispatcher
	if len(segs) >= 3 && segs[0] == "ws" { // ws, device, service
		device := segs[1]
		// If the incoming path already includes a mac: style prefix and DEST_PREFIX will add another,
		// strip the existing one to avoid duplication like mac:mac:<id>/service.
		// We only handle the simple case where the prefix matches exactly (case-sensitive) and contains ':'.
		service := segs[2]
		// If base dispatcher is a *rpc.WRPDispatcher clone with device destination
		if base, ok := h.Dispatcher.(*rpc.WRPDispatcher); ok {
			dcopy := *base // shallow copy safe (contains pointers we reuse intentionally: Client)
			prefix := os.Getenv("DEST_PREFIX")
			if prefix == "" {
				prefix = "mac:" // default
			}
			if strings.HasPrefix(device, prefix) {
				orig := device
				device = strings.TrimPrefix(device, prefix)
				log.Printf("normalized device prefix: original=%s normalized=%s prefix=%s", orig, device, prefix)
			}
			// Canonical service name that the device actually registered with Parodus (default BlizzardRDK)
			canonical := os.Getenv("CANONICAL_SERVICE_NAME")
			if canonical == "" {
				canonical = "BlizzardRDK"
			}
			// Destination must always use canonical; the path-provided service may be an alias.
			dcopy.Dest = prefix + device + "/" + canonical
			dcopy.ServiceName = canonical
			log.Printf("connection bound device=%s pathService=%s canonicalService=%s dest=%s", device, service, canonical, dcopy.Dest)
			dispatcher = &dcopy

			// Fallback services support: DEST_SERVICE_FALLBACKS=svc1,svc2
			if fb := os.Getenv("DEST_SERVICE_FALLBACKS"); fb != "" {
				// Build service list: canonical first, then alias (if different), then fallbacks
				parts := []string{canonical}
				if service != "" && service != canonical {
					parts = append(parts, service)
				}
				for _, p := range strings.Split(fb, ",") {
					p = strings.TrimSpace(p)
					if p != "" && p != canonical && p != service {
						parts = append(parts, p)
					}
				}
				dispatcher = &rpc.MultiServiceDispatcher{Client: dcopy.Client, Source: dcopy.Source, DeviceID: device, DestPrefix: prefix, Services: parts}
				log.Printf("multi-service fallback enabled device=%s services=%v (canonical=%s)", device, parts, canonical)
			}
		}
	}
	cl := &client{conn: c}
	go cl.run(dispatcher, h.Bus)
}

func (c *client) run(d rpc.Dispatcher, bus *events.Bus) {
	defer c.conn.Close()
	// Reader setup
	c.conn.SetReadLimit(512 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Subscribe to events (if bus provided)
	var evCh <-chan events.Event
	var cancel func()
	if bus != nil {
		_, ch, cfn := bus.Subscribe(64)
		evCh = ch
		cancel = cfn
	}
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	// Ping loop (keepalive)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.mu.Lock()
				_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					c.mu.Unlock()
					return
				}
				c.mu.Unlock()
			case <-done:
				return
			}
		}
	}()

	// Event forwarder loop
	go func() {
		for {
			select {
			case ev, ok := <-evCh:
				if !ok {
					return
				}
				c.writeJSON(rpc.Notification{JSONRPC: "2.0", Method: buildEventMethod(ev), Params: map[string]any{"device": ev.Device, "service": ev.Service, "event": ev.Name, "payload": string(ev.Payload)}})
			case <-done:
				return
			}
		}
	}()

	// Read loop
	for {
		mt, message, err := c.conn.ReadMessage()
		if err != nil {
			close(done)
			return
		}
		if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
			continue
		}
		req, perr := rpc.ParseRequest(message)
		if perr != nil {
			c.writeError(nil, -32600, perr.Error())
			continue
		}
		resp := d.Handle(req)
		if resp != nil {
			c.writeJSON(resp)
		}
		if gatewayAckEnabled() { // synthetic gateway ack (optional)
			c.writeJSON(rpc.Notification{JSONRPC: "2.0", Method: "Gateway.Ack", Params: map[string]any{"correlationId": string(req.ID), "id": uuid.NewString()}})
		}
	}
}

// gatewayAckEnabled returns true when synthetic Gateway.Ack notifications should be emitted.
// New Behavior: default (unset variable) = DISABLED to reduce noise and mirror direct device connections.
// Enable by setting GATEWAY_ACK to 1, true, yes, on (case-insensitive). Any other value (including unset) disables.
func gatewayAckEnabled() bool {
	v := os.Getenv("GATEWAY_ACK")
	if v == "" { // default OFF
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	}
	return false
}

func buildEventMethod(ev events.Event) string {
	name := ev.Name
	if name == "" {
		name = "Unknown"
	}
	service := ev.Service
	if service == "" {
		service = "BlizzardRDK"
	}
	return "rpc.Event." + service + "." + name
}

func (c *client) writeError(id []byte, code int, msg string) {
	resp := rpc.Response{JSONRPC: "2.0", ID: id, Error: &rpc.Error{Code: code, Message: msg}}
	c.writeJSON(resp)
}

func (c *client) writeJSON(v interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Refresh per-message write deadline to avoid stale timeout from prior ping when queue backs up.
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.conn.WriteJSON(v); err != nil {
		// Provide more diagnostic context for timeouts vs other errors.
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			log.Printf("write timeout (deadline exceeded) err=%v", err)
			return
		}
		// Unwrap if wrapped by websocket library
		if errors.Is(err, os.ErrDeadlineExceeded) {
			log.Printf("write deadline exceeded err=%v", err)
			return
		}
		log.Printf("write error: %T %v", err, err)
	}
}
