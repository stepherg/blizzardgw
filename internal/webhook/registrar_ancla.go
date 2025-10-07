package webhook

// ancla/chrysom based registrar. This file re-introduces richer webhook registration
// semantics using the upstream XMiDT libraries. Dependencies must be added to go.mod
// (github.com/xmidt-org/ancla, github.com/xmidt-org/argus). Until resolved, builds will fail.
// TODO: Add module requirements and run `go get` once dependency policy is finalized.

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/xmidt-org/ancla"
	"github.com/xmidt-org/ancla/auth"
	"github.com/xmidt-org/ancla/chrysom"
	"github.com/xmidt-org/ancla/schema"
	webhook "github.com/xmidt-org/webhook-schema"
)

// createBasicAuthDecorator creates a simple auth decorator that adds Basic authentication
func createBasicAuthDecorator(authHeader string) auth.Decorator {
	return auth.DecoratorFunc(func(ctx context.Context, req *http.Request) error {
		if authHeader != "" {
			// If the auth header doesn't start with "Basic ", add it
			if !strings.HasPrefix(authHeader, "Basic ") {
				authHeader = "Basic " + authHeader
			}
			req.Header.Set("Authorization", authHeader)
		}
		return nil
	})
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

	// Setup defaults
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

	retries := c.Retries
	if retries <= 0 {
		retries = 3
	}

	var attempt func(int)
	attempt = func(remaining int) {
		log.Printf("webhook(ancla): registering callback=%s remaining=%d", c.CallbackURL, remaining)

		// Create chrysom client options
		clientOpts := []chrysom.ClientOption{
			chrysom.StoreBaseURL(c.ArgusURL),
			chrysom.Bucket(bucket),
		}

		// Add auth if provided
		if c.AuthBasic != "" {
			authDecorator := createBasicAuthDecorator(c.AuthBasic)
			clientOpts = append(clientOpts, chrysom.Auth(authDecorator))
		}

		// Create the chrysom client
		client, err := chrysom.NewBasicClient(clientOpts...)
		if err != nil {
			log.Printf("webhook(ancla): client init error: %v", err)
			retryLater(remaining, attempt)
			return
		}

		// Create the ancla service
		svc := ancla.NewService(client)

		// Build the webhook registration using the schema types
		until := time.Now().Add(duration)

		// Create field regex for device matching
		var matchers []webhook.FieldRegex
		for _, devicePattern := range devices {
			matchers = append(matchers, webhook.FieldRegex{
				Regex: devicePattern,
				Field: "device_id",
			})
		}

		// Create the registration V2
		registration := webhook.RegistrationV2{
			CanonicalName: "blizzard-gateway-webhook",
			Address:       "blizzard-gateway",
			Webhooks: []webhook.Webhook{
				{
					ReceiverURLs: []string{c.CallbackURL},
					Accept:       "application/json",
				},
			},
			Matcher: matchers,
			Expires: until,
		}

		// Create the manifest
		manifest := &schema.ManifestV2{
			Registration: registration,
		}

		// Register the webhook
		if err := svc.Add(ctx, "", manifest); err != nil {
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
