package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	wrp "github.com/xmidt-org/wrp-go/v3"
)

type fakeWRPClient struct{ attempts int }

func (f *fakeWRPClient) Do(ctx context.Context, m *wrp.Message) (*wrp.Message, error) {
	f.attempts++
	switch f.attempts {
	case 1:
		return nil, errors.New("network unreachable")
	case 2:
		resp := Response{JSONRPC: "2.0", ID: json.RawMessage(`"abc"`), Result: map[string]any{"svc": m.ServiceName}}
		b, _ := json.Marshal(resp)
		return &wrp.Message{Payload: b, ContentType: "application/json"}, nil
	default:
		return nil, errors.New("unexpected extra attempt")
	}
}

func TestMultiServiceDispatcherFallback(t *testing.T) {
	f := &fakeWRPClient{}
	d := &MultiServiceDispatcher{Client: f, Source: "src", DeviceID: "dev1", DestPrefix: "mac:", Services: []string{"BlizzardRDK", "config"}, Timeout: 100 * time.Millisecond}
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage(`"abc"`), Method: "Device.Ping"}
	resp := d.Handle(req)
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected success, got %+v", resp)
	}
	m, _ := resp.Result.(map[string]any)
	if m["svc"] != "config" { // second service should have succeeded
		t.Fatalf("expected fallback service 'config', got %v", m["svc"])
	}
	if f.attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", f.attempts)
	}
}

// (proxy type removed; fake implements interface directly)
