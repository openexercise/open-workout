package conformance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	openworkout "github.com/openexercise/open-workout"
)

// SignatureHeader is the header a webhook delivery is signed in.
const SignatureHeader = "OWS-Signature"

// Sign computes a delivery's OWS-Signature: "t=<unix seconds>,v1=<hex HMAC-SHA256>", the HMAC
// keyed with the subscription's secret over "<t>.<body>".
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return "t=" + ts + ",v1=" + mac(secret, ts, body)
}

func mac(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// VerifySignature checks a webhook delivery as a receiver should: the OWS-Signature header's v1
// is the HMAC of the body sent at its t, compared in constant time, and t is within tolerance of
// now, so a captured delivery can't be replayed later.
func VerifySignature(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return errors.New("the signature header needs t=<unix seconds> and v1=<hex HMAC>")
	}
	if d := now.Sub(time.Unix(t, 0)); d > tolerance || d < -tolerance {
		return fmt.Errorf("signed at %s, more than %s from now", time.Unix(t, 0).UTC().Format(time.RFC3339), tolerance)
	}
	want := mac(secret, ts, body)
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return errors.New("no v1 signature matches the body")
}

// SignatureVector is one published webhook signature case (schema/v0/webhook-signature-vectors.json).
type SignatureVector struct {
	Name      string `json:"name"`
	Secret    string `json:"secret"`
	Timestamp int64  `json:"timestamp"`
	Body      string `json:"body"`
	Signature string `json:"signature"`
}

// SignatureVectors reads the published webhook signature cases, for a sender's own tests: sign
// each body at its timestamp and compare with Signature.
func SignatureVectors() ([]SignatureVector, error) {
	raw, err := openworkout.Schemas.ReadFile("schema/v0/webhook-signature-vectors.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Cases []SignatureVector `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("webhook signature vectors: %w", err)
	}
	return doc.Cases, nil
}
