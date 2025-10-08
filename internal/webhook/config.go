package webhook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Config holds configuration for raw Argus webhook registration.
// We store a webhook spec as an Argus item in a bucket (default hooks).
type Config struct {
	Enable         bool
	ArgusURL       string
	Bucket         string
	AuthBasic      string
	CallbackURL    string
	Events         []string
	DeviceMatchers []string
	Duration       time.Duration // retains for documentation (not used directly by Argus storage)
	TTL            int           // seconds for Argus item ttl (0 => default 24h server side)
	Retries        int
}

// Item is the payload sent to Argus store bucket.
type Item struct {
	ID   string      `json:"id"`
	Data interface{} `json:"data"`
	TTL  int         `json:"ttl,omitempty"`
}

// WebhookData is the opaque data stored (convention between fanout service and gateway).
type WebhookData struct {
	Callback string   `json:"callback"`
	Events   []string `json:"events"`
	Devices  []string `json:"devices"`
}

// LegacyWebhookConfig represents the webhook configuration in Caduceus legacy format
type LegacyWebhookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

// LegacyWebhookMatcher represents device ID matching in legacy format
type LegacyWebhookMatcher struct {
	DeviceID []string `json:"device_id"`
}

// LegacyWebhook represents the webhook structure that Caduceus understands
type LegacyWebhook struct {
	Config                LegacyWebhookConfig  `json:"config"`
	Events                []string             `json:"events"`
	Matcher               LegacyWebhookMatcher `json:"matcher"`
	FailureURL            string               `json:"failure_url"`
	Duration              int64                `json:"duration"`
	Until                 time.Time            `json:"until"`
	RegisteredFromAddress string               `json:"registered_from_address,omitempty"`
}

// LegacyWebhookData is the data structure that Caduceus expects
type LegacyWebhookData struct {
	PartnerIDs []string       `json:"PartnerIDs"`
	Webhook    *LegacyWebhook `json:"Webhook"`
}

// Register stores/updates the webhook spec in Argus via PUT /store/<bucket>/<id>.
func (c Config) Register() {
	if !c.Enable {
		log.Printf("webhook: disabled")
		return
	}
	if c.ArgusURL == "" || c.CallbackURL == "" {
		log.Printf("webhook: missing ARGUS_URL or WEBHOOK_URL")
		return
	}
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
	retries := c.Retries
	if retries <= 0 {
		retries = 3
	}

	duration := c.Duration
	if duration <= 0 {
		duration = time.Duration(0xffff) * time.Hour // ~7.5 years
	}
	until := time.Now().Add(duration)

	// Deterministic ID based on callback.
	h := sha256.Sum256([]byte(strings.ToLower(c.CallbackURL)))
	id := hex.EncodeToString(h[:])

	// Create webhook in LEGACY format that Caduceus understands
	legacyWebhook := LegacyWebhook{
		Config: LegacyWebhookConfig{
			URL:         c.CallbackURL,
			ContentType: "application/msgpack",
		},
		Events: events,
		Matcher: LegacyWebhookMatcher{
			DeviceID: devices,
		},
		FailureURL:            "",
		Duration:              int64(duration),
		Until:                 until,
		RegisteredFromAddress: "blizzard-gateway",
	}

	legacyData := LegacyWebhookData{
		PartnerIDs: nil,
		Webhook:    &legacyWebhook,
	}

	item := Item{ID: id, Data: legacyData}
	if c.TTL > 0 {
		item.TTL = c.TTL
	}

	body, _ := json.Marshal(item)
	url := fmt.Sprintf("%s/api/v1/store/%s/%s", strings.TrimRight(c.ArgusURL, "/"), bucket, id)

	var attempt func(int)
	attempt = func(remaining int) {
		log.Printf("webhook: registering id=%s callback=%s remaining=%d", id, c.CallbackURL, remaining)
		req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(body)))
		if err != nil {
			log.Printf("webhook: new request error: %v", err)
			retry(remaining, attempt)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if c.AuthBasic != "" {
			req.Header.Set("Authorization", c.AuthBasic)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("webhook: request error: %v", err)
			retry(remaining, attempt)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			log.Printf("webhook: unexpected status %d", resp.StatusCode)
			retry(remaining, attempt)
			return
		}
		log.Printf("webhook: registered ok status=%d id=%s", resp.StatusCode, id)
	}
	attempt(retries)
}

func retry(remaining int, f func(int)) {
	if remaining <= 0 {
		log.Printf("webhook: max retries exhausted")
		return
	}
	time.AfterFunc(5*time.Second, func() { f(remaining - 1) })
}
