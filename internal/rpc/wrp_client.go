package rpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	wrp "github.com/xmidt-org/wrp-go/v3"
)

// WRPClient performs HTTP POST of msgpack encoded WRP messages to Scytale.
type WRPClient struct {
	Client        *http.Client
	URL           string
	Authorization string // optional bearer token
}

var (
	// ErrBadStatus indicates a non-2xx response from upstream.
	ErrBadStatus = errors.New("upstream returned non-2xx status")
)

// Do sends a WRP message and decodes the WRP response.
func (wc *WRPClient) Do(ctx context.Context, m *wrp.Message) (*wrp.Message, error) {
	if wc.Client == nil {
		wc.Client = &http.Client{Timeout: 10 * time.Second}
	}
	buf := &bytes.Buffer{}
	if err := wrp.NewEncoder(buf, wrp.Msgpack).Encode(m); err != nil {
		return nil, fmt.Errorf("encode wrp: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wc.URL, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/msgpack")
	if wc.Authorization != "" {
		auth := strings.TrimSpace(wc.Authorization)
		lower := strings.ToLower(auth)
		// If it already starts with a known auth scheme, pass through.
		if !(strings.HasPrefix(lower, "basic ") || strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "digest ")) {
			auth = "Basic " + auth
		}
		req.Header.Set("Authorization", auth)
	}
	resp, err := wc.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: %s", ErrBadStatus, string(body))
	}
	var out wrp.Message
	if err := wrp.NewDecoder(resp.Body, wrp.Msgpack).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode wrp: %w", err)
	}
	return &out, nil
}
