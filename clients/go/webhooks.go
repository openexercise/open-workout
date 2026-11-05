package openworkout

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader carries a delivery's signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of
// "<t>.<body>" with the subscription's secret>.
const SignatureHeader = "OWS-Signature"

// LifterclubSignatureHeader is the same signature under its earlier name, which Lifterclub also
// sends until OWS v1.
const LifterclubSignatureHeader = "Lifterclub-Signature"

// CreateWebhook subscribes the app to changes in the user's log. The returned Secret is shown
// once: keep it to verify deliveries. Only a connected app can subscribe, at most 5 per app and
// account.
func (c *Client) CreateWebhook(ctx context.Context, in WebhookInput) (*Webhook, error) {
	var out Webhook
	return &out, c.Do(ctx, http.MethodPost, "/v1/webhooks", nil, in, &out)
}

// ListWebhooks lists the app's subscriptions on the account, without secrets.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out struct {
		Webhooks []Webhook `json:"webhooks"`
	}
	return out.Webhooks, c.Do(ctx, http.MethodGet, "/v1/webhooks", nil, nil, &out)
}

// DeleteWebhook ends a subscription.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/v1/webhooks/"+pathID(id), nil, nil, nil)
}

// VerifySignature checks a delivery's signature header against the raw body with the
// subscription's secret, refusing a timestamp further than tolerance from now (five minutes is
// the documented choice). Check it before reading the body as JSON.
func VerifySignature(secret, header string, body []byte, now time.Time, tolerance time.Duration) bool {
	var t int64
	var v1 string
	for part := range strings.SplitSeq(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			t, _ = strconv.ParseInt(value, 10, 64)
		case "v1":
			v1 = value
		}
	}
	if t == 0 || v1 == "" || now.Sub(time.Unix(t, 0)).Abs() > tolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(t, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	want, err := hex.DecodeString(v1)
	return err == nil && hmac.Equal(want, mac.Sum(nil))
}
