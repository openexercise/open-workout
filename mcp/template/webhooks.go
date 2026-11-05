package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Hooks keeps each person's webhook subscriptions and delivers a signed event for every change to
// a workout. A real server keeps subscriptions in its database, delivers from a queue that
// retries, checks on every delivery that the address it reached is public, and pauses a
// subscription that keeps failing.
type Hooks struct {
	mu        sync.Mutex
	subs      map[string][]*Subscription // by person
	client    *http.Client
	allowHTTP bool // for local development and tests only: production endpoints are https
	wg        sync.WaitGroup
}

// Subscription is one webhook. Secret is shown only when it's created.
type Subscription struct {
	ID                  string     `json:"id"`
	URL                 string     `json:"url"`
	Events              []string   `json:"events"`
	CreatedAt           time.Time  `json:"created_at"`
	LastDeliveredAt     *time.Time `json:"last_delivered_at,omitempty"`
	LastStatus          *int       `json:"last_status,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	Secret              string     `json:"secret,omitempty"`
}

// maxSubscriptions bounds one person's webhooks.
const maxSubscriptions = 5

var webhookEvents = []string{"workout.created", "workout.updated", "workout.deleted"}

// NewHooks makes an empty set of subscriptions.
func NewHooks(allowHTTP bool) *Hooks {
	return &Hooks{subs: map[string][]*Subscription{}, client: &http.Client{Timeout: 10 * time.Second}, allowHTTP: allowHTTP}
}

// Subscribe adds a webhook and returns it with its secret, the only time the secret is shown.
func (h *Hooks) Subscribe(person, rawURL string, events []string) (Subscription, error) {
	u, err := url.Parse(rawURL)
	if err != nil || len(rawURL) > 2048 || u.Host == "" || u.User != nil || u.Fragment != "" ||
		(u.Scheme != "https" && !(h.allowHTTP && u.Scheme == "http")) {
		return Subscription{}, invalid("url", "an https URL with a host, no credentials and no fragment")
	}
	if len(events) == 0 {
		return Subscription{}, invalid("events", "name at least one of %v", webhookEvents)
	}
	for _, e := range events {
		if !slices.Contains(webhookEvents, e) {
			return Subscription{}, invalid("events", "%q isn't one of %v", e, webhookEvents)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs[person]) >= maxSubscriptions {
		return Subscription{}, invalid("url", "at most %d webhooks", maxSubscriptions)
	}
	sub := &Subscription{ID: newID("wh_"), URL: rawURL, Events: slices.Clone(events), CreatedAt: time.Now().UTC(), Secret: "whsec_" + rand.Text()}
	h.subs[person] = append(h.subs[person], sub)
	return *sub, nil
}

// List returns a person's webhooks, without their secrets.
func (h *Hooks) List(person string) []Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Subscription{}
	for _, s := range h.subs[person] {
		c := *s
		c.Secret = ""
		out = append(out, c)
	}
	return out
}

// Delete removes a webhook, and says whether there was one.
func (h *Hooks) Delete(person, id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	before := len(h.subs[person])
	h.subs[person] = slices.DeleteFunc(h.subs[person], func(s *Subscription) bool { return s.ID == id })
	return len(h.subs[person]) < before
}

// Notify delivers an event to each of the person's subscriptions to it, in the background: it's
// called while the store is locked, so it must not wait.
func (h *Hooks) Notify(person, event, workoutID string) {
	h.mu.Lock()
	var targets []Subscription
	for _, s := range h.subs[person] {
		if slices.Contains(s.Events, event) {
			targets = append(targets, *s)
		}
	}
	h.mu.Unlock()
	occurred := time.Now().UTC()
	for _, s := range targets {
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.deliver(person, s, event, workoutID, occurred)
		}()
	}
}

// Wait waits for deliveries in flight, for tests and shutdown.
func (h *Hooks) Wait() { h.wg.Wait() }

// deliver POSTs one event, signed, trying three times.
func (h *Hooks) deliver(person string, s Subscription, event, workoutID string, occurred time.Time) {
	deliveryID := newID("dl_")
	body, _ := json.Marshal(map[string]any{
		"event": event, "workout_id": workoutID, "occurred_at": occurred, "subscription_id": s.ID, "delivery_id": deliveryID,
	})
	status := 0
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, err := http.NewRequest(http.MethodPost, s.URL, bytes.NewReader(body))
		if err != nil {
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "open-workout-template")
		req.Header.Set("OWS-Event", event)
		req.Header.Set("OWS-Delivery", deliveryID)
		req.Header.Set("OWS-Signature", sign(s.Secret, time.Now(), body))
		res, err := h.client.Do(req)
		if err != nil {
			status = 0
			continue
		}
		res.Body.Close()
		if status = res.StatusCode; status < 300 {
			break
		}
	}
	h.record(person, s.ID, status)
}

func (h *Hooks) record(person, id string, status int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs[person] {
		if s.ID != id {
			continue
		}
		now := time.Now().UTC()
		s.LastDeliveredAt, s.LastStatus = &now, &status
		if status >= 200 && status < 300 {
			s.ConsecutiveFailures = 0
		} else {
			s.ConsecutiveFailures++
			log.Printf("webhook %s: delivery failed with status %d", id, status)
		}
	}
}

// sign is OWS-Signature: "t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">", keyed with the
// subscription's secret (schema/v0/webhook-signature-vectors.json has worked cases).
func sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}
