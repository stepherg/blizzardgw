package webhook

// ancla/chrysom based registrar. This file re-introduces richer webhook registration
// semantics using the upstream XMiDT libraries. Dependencies must be added to go.mod
// (github.com/xmidt-org/ancla, github.com/xmidt-org/argus). Until resolved, builds will fail.
// TODO: Add module requirements and run `go get` once dependency policy is finalized.

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/xmidt-org/ancla"
	"github.com/xmidt-org/argus/chrysom"
)

// AnclaConfig maps our existing Config into ancla client pieces.
func (c Config) toAncla() (ancla.Config, []string, []string, time.Duration) {
	bucket := c.Bucket
	if bucket == "" {
		bucket = "hooks"
	}
	events := c.Events
	if len(events) == 0 {
		events = []string{".*"}
	}
	devices := c.DeviceMatchers
	if len(devices) == 0 {
		devices = []string{".*"}
	}
	duration := c.Duration
	if duration <= 0 {
		duration = time.Duration(0xffff) * time.Hour
	}
	ac := ancla.Config{
		JWTParserType:     "simple",
		DisablePartnerIDs: true,
		BasicClientConfig: chrysom.BasicClientConfig{
			Address:    c.ArgusURL,
			Bucket:     bucket,
			Auth:       chrysom.Auth{Basic: c.AuthBasic},
			HTTPClient: &http.Client{Timeout: 30 * time.Second},
		},
	}
	return ac, events, devices, duration
}

// RegisterAncla performs registration using ancla instead of raw Argus PUT.
func (c Config) RegisterAncla(ctx context.Context) {
	if !c.Enable {
		log.Printf("webhook(ancla): disabled")
		return
	}
	if c.ArgusURL == "" || c.CallbackURL == "" {
		log.Printf("webhook(ancla): missing Argus URL or callback")
		return
	}
	acfg, events, devices, duration := c.toAncla()
	retries := c.Retries
	if retries <= 0 {
		retries = 3
	}
	var attempt func(int)
	attempt = func(remaining int) {
		log.Printf("webhook(ancla): registering callback=%s remaining=%d", c.CallbackURL, remaining)
		svc, err := ancla.NewService(acfg, nil)
		if err != nil {
			log.Printf("webhook(ancla): service init error: %v", err)
			retryLater(remaining, attempt)
			return
		}
		until := time.Now().Add(duration)
		hook := ancla.InternalWebhook{Webhook: ancla.Webhook{Config: ancla.DeliveryConfig{URL: c.CallbackURL, ContentType: "application/json"}, Events: events, Matcher: ancla.MetadataMatcherConfig{DeviceID: devices}, Duration: duration, Until: until}}
		if err := svc.Add(ctx, "", hook); err != nil {
			log.Printf("webhook(ancla): add error: %v", err)
			retryLater(remaining, attempt)
			return
		}
		log.Printf("webhook(ancla): registered ok until=%s", until.Format(time.RFC3339))
	}
	attempt(retries)
}

// retryLater mirrors the earlier helper (not exported here).
func retryLater(remaining int, f func(int)) {
	if remaining <= 0 {
		log.Printf("webhook(ancla): max retries exhausted")
		return
	}
	time.AfterFunc(5*time.Second, func() { f(remaining - 1) })
}
