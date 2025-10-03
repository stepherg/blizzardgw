package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	wrp "github.com/xmidt-org/wrp-go/v3"
)

// TestWRPDispatcherRoundTrip validates JSON-RPC -> WRP -> JSON-RPC translation.
func TestWRPDispatcherRoundTrip(t *testing.T) {
	// Mock Scytale endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		dec := wrp.NewDecoder(r.Body, wrp.Msgpack)
		var inbound wrp.Message
		if err := dec.Decode(&inbound); err != nil {
			t.Fatalf("decode inbound wrp: %v", err)
		}
		if inbound.Type != wrp.SimpleRequestResponseMessageType {
			t.Fatalf("unexpected type: %v", inbound.Type)
		}
		// Craft JSON-RPC response referencing same ID/TransactionUUID.
		var reqObj map[string]any
		_ = json.Unmarshal(inbound.Payload, &reqObj)
		jr := map[string]any{"jsonrpc": "2.0", "id": reqObj["id"], "result": map[string]any{"ok": true, "method": reqObj["method"]}}
		payload, _ := json.Marshal(jr)
		out := wrp.Message{Type: wrp.SimpleRequestResponseMessageType, TransactionUUID: inbound.TransactionUUID, Payload: payload, ContentType: "application/json"}
		buf := &bytes.Buffer{}
		if err := wrp.NewEncoder(buf, wrp.Msgpack).Encode(&out); err != nil {
			t.Fatalf("encode outbound: %v", err)
		}
		w.Header().Set("Content-Type", "application/msgpack")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	d := &WRPDispatcher{Client: &WRPClient{URL: srv.URL}, Source: "blizzard/gateway"}

	// Build JSON-RPC request
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage(`"abc123"`), Method: "Device.Ping"}
	resp := d.Handle(req)
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected success response, got %+v", resp)
	}
	// Validate echo method in result
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	if m["method"] != "Device.Ping" {
		t.Fatalf("unexpected method echo: %v", m["method"])
	}
	if m["ok"] != true {
		t.Fatalf("expected ok true")
	}
}
