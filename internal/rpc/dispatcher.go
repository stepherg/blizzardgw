package rpc

import (
	"encoding/json"
	"errors"
	"time"
)

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error matches JSON-RPC error object.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Notification is a server->client message without an ID.
type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Dispatcher processes JSON-RPC requests.
type Dispatcher interface {
	Handle(*Request) *Response
}

// EchoDispatcher simple implementation returning static structure.
// Intended placeholder for routing to device / WRP layer.
type EchoDispatcher struct{}

func (e EchoDispatcher) Handle(r *Request) *Response {
	if r.Method == "" {
		return &Response{JSONRPC: "2.0", ID: r.ID, Error: &Error{Code: -32600, Message: "invalid request"}}
	}
	// Simulate a tiny processing delay
	time.Sleep(5 * time.Millisecond)
	return &Response{JSONRPC: "2.0", ID: r.ID, Result: map[string]interface{}{"echo": true, "method": r.Method}}
}

// ParseRequest decodes raw JSON into Request with basic validation.
func ParseRequest(raw []byte) (*Request, error) {
	var r Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.JSONRPC != "2.0" {
		return nil, errors.New("unsupported jsonrpc version")
	}
	if r.Method == "" {
		return nil, errors.New("method required")
	}
	return &r, nil
}
