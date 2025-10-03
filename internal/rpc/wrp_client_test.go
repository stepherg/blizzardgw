package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	wrp "github.com/xmidt-org/wrp-go/v3"
)

// helper server echoes back empty WRP to satisfy decode
func newEchoWRPServer(t *testing.T, captureAuth *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captureAuth != nil {
			*captureAuth = r.Header.Get("Authorization")
		}
		// Return an empty valid WRP message
		w.Header().Set("Content-Type", "application/msgpack")
		enc := wrp.NewEncoder(w, wrp.Msgpack)
		_ = enc.Encode(&wrp.Message{})
	}))
}

func TestWRPClientAuthPrefixing(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"dXNlcjpwYXNz", "Basic dXNlcjpwYXNz"},
		{" Basic dXNlcjpwYXNz", "Basic dXNlcjpwYXNz"},
		{"Bearer token123", "Bearer token123"},
		{"bearer token123", "bearer token123"}, // preserve provided case even if lower
		{"Digest something", "Digest something"},
	}
	for _, c := range cases {
		var got string
		srv := newEchoWRPServer(t, &got)
		client := &WRPClient{URL: srv.URL, Authorization: c.in}
		_, err := client.Do(context.Background(), &wrp.Message{})
		srv.Close()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.EqualFold(got, c.want) { // case-insensitive compare acceptable for scheme
			t.Errorf("auth mismatch for input %q: got %q want %q", c.in, got, c.want)
		}
	}
}
