package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	wrp "github.com/xmidt-org/wrp-go/v3"
)

// WRPDispatcher converts JSON-RPC into WRP SimpleRequestResponse messages.
// JSON-RPC id is mapped to TransactionUUID. The WRP payload (Content) carries
// the original JSON-RPC request (as JSON) so the device / downstream runtime
// can parse it. A response is expected with a JSON payload containing either
// result or error per JSON-RPC spec, which is forwarded unchanged.
type WRPDispatcher struct {
	Client      *WRPClient
	Source      string // e.g., "blizzard/gateway"
	Dest        string // device destination (logical) optional for now
	ServiceName string // optional path/service identifier
}

// Handle implements Dispatcher.
func (w *WRPDispatcher) Handle(r *Request) *Response {
	// Marshal request back to JSON for embedding in WRP content.
	raw, err := json.Marshal(r)
	if err != nil {
		return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32603, Message: "marshal request failed", Data: err.Error()}}
	}
	// Build WRP message.
	msg := &wrp.Message{
		Type:            wrp.SimpleRequestResponseMessageType,
		Source:          w.Source,
		Destination:     w.Dest,
		ServiceName:     w.ServiceName,
		TransactionUUID: string(r.ID),
		ContentType:     "application/json",
		Payload:         raw,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	upstream, err := w.Client.Do(ctx, msg)
	if err != nil {
		// Enrich Data with destination for debugging
		detail := fmt.Sprintf("dest=%s service=%s err=%s", w.Dest, w.ServiceName, err.Error())
		return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32100, Message: "transport error", Data: detail}}
	}
	// Attempt to parse upstream payload as JSON-RPC response; if that fails treat as raw result.
	var jr Response
	if err := json.Unmarshal(upstream.Payload, &jr); err == nil && jr.JSONRPC == "2.0" { // well-formed response
		// Ensure ID fallback if missing.
		if len(jr.ID) == 0 {
			jr.ID = r.ID
		}
		return &jr
	}
	// Fallback: wrap raw bytes as result
	return &Response{JSONRPC: "2.0", ID: r.ID, Result: json.RawMessage(upstream.Payload)}
}
