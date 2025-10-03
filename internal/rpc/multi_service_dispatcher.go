package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	wrp "github.com/xmidt-org/wrp-go/v3"
)

// MultiServiceDispatcher attempts a JSON-RPC request across multiple service
// name candidates (first success or first non-transport error wins). It builds
// WRP SimpleRequestResponse messages directly rather than chaining through
// WRPDispatcher to allow per-attempt Dest/ServiceName changes.
//
// A "transport error" is inferred from a Go error returned by Client.Do.
// If a response payload is received and it decodes to a JSON-RPC error whose
// code != -32100, that is considered a terminal (routing succeeded) outcome.
//
// Use cases: fallback from an expected service (e.g. BlizzardRDK) to legacy
// service (e.g. config) while the device software transitions.
// WRPDoer is the minimal interface needed from a WRP client.
type WRPDoer interface {
	Do(context.Context, *wrp.Message) (*wrp.Message, error)
}

type MultiServiceDispatcher struct {
	Client     WRPDoer
	Source     string
	DeviceID   string
	DestPrefix string // e.g. "mac:" (may be empty)
	Services   []string
	Timeout    time.Duration // per-attempt timeout (default 8s)
}

func (m *MultiServiceDispatcher) Handle(r *Request) *Response {
	if len(m.Services) == 0 {
		return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32603, Message: "no services configured"}}
	}
	if m.Client == nil {
		return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32603, Message: "no client configured"}}
	}
	if m.Timeout <= 0 {
		m.Timeout = 8 * time.Second
	}
	rawReq, err := json.Marshal(r)
	if err != nil {
		return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32603, Message: "marshal request failed", Data: err.Error()}}
	}
	var lastErr error
	var attempts []map[string]string
	for _, svc := range m.Services {
		dest := fmt.Sprintf("%s%s/%s", m.DestPrefix, m.DeviceID, svc)
		msg := &wrp.Message{
			Type:            wrp.SimpleRequestResponseMessageType,
			Source:          m.Source,
			Destination:     dest,
			ServiceName:     svc,
			TransactionUUID: string(r.ID),
			ContentType:     "application/json",
			Payload:         rawReq,
		}
		ctx, cancel := context.WithTimeout(context.Background(), m.Timeout)
		upstream, sendErr := m.Client.Do(ctx, msg)
		cancel()
		if sendErr != nil {
			lastErr = fmt.Errorf("svc=%s dest=%s err=%w", svc, dest, sendErr)
			attempts = append(attempts, map[string]string{"service": svc, "status": "transport_error"})
			continue
		}
		// Attempt to decode a JSON-RPC response.
		var jr Response
		if err := json.Unmarshal(upstream.Payload, &jr); err == nil && jr.JSONRPC == "2.0" {
			if len(jr.ID) == 0 {
				jr.ID = r.ID
			}
			return &jr
		}
		// If payload isn't JSON-RPC, wrap as a success result blob.
		return &Response{JSONRPC: "2.0", ID: r.ID, Result: json.RawMessage(upstream.Payload)}
	}
	return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32100, Message: "transport error", Data: fmt.Sprintf("attempts=%v last=%v", attempts, lastErr)}}
}
