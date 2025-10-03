package ws

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stepherg/blizzardgw/internal/rpc"
)

func TestWebSocketEcho(t *testing.T) {
	h := &Handler{Dispatcher: rpc.EchoDispatcher{}}
	srv := httptest.NewServer(h)
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/"

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		// try /ws path variant if root fails
		u.Path = "/ws"
		c, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
	}
	defer c.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": "1", "method": "Device.Ping", "params": map[string]any{"ts": time.Now().Unix()}}
	if err := c.WriteJSON(req); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Expect response then notification
	var gotResp bool
	var gotNote bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (!gotResp || !gotNote) {
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var base map[string]json.RawMessage
		if err := json.Unmarshal(data, &base); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := base["id"]; ok {
			gotResp = true
		} else if _, ok := base["method"]; ok {
			gotNote = true
		}
	}
	if !gotResp {
		t.Fatalf("did not receive response")
	}
	if !gotNote {
		t.Fatalf("did not receive notification")
	}
}
