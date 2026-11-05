package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSignMatchesTheVectors signs the published cases (../../schema/v0, skipped in a copy that
// doesn't have them).
func TestSignMatchesTheVectors(t *testing.T) {
	raw, err := os.ReadFile("../../schema/v0/webhook-signature-vectors.json")
	if err != nil {
		t.Skip("no published vectors next to this copy")
	}
	var doc struct {
		Cases []struct {
			Name, Secret, Body, Signature string
			Timestamp                     int64
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Cases {
		if got := sign(c.Secret, time.Unix(c.Timestamp, 0), []byte(c.Body)); got != c.Signature {
			t.Errorf("%s: %s, want %s", c.Name, got, c.Signature)
		}
	}
}

// TestWebhookDelivery: a change to a workout reaches each subscription to its event, signed with
// the secret shown at creation; another person's change doesn't.
func TestWebhookDelivery(t *testing.T) {
	var mu sync.Mutex
	var got []*http.Request
	var bodies [][]byte
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got, bodies = append(got, r), append(bodies, b)
		mu.Unlock()
	}))
	defer receiver.Close()

	catalogue, err := LoadCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(catalogue, "kg", time.UTC)
	hooks := NewHooks(true)
	store.notify = hooks.Notify
	if _, err := hooks.Subscribe("me", "ftp://x", []string{"workout.created"}); err == nil {
		t.Error("an ftp URL was accepted")
	}
	if _, err := NewHooks(false).Subscribe("me", receiver.URL, []string{"workout.created"}); err == nil {
		t.Error("an http URL was accepted without -allow-http-webhooks")
	}
	sub, err := hooks.Subscribe("me", receiver.URL, []string{"workout.created", "workout.deleted"})
	if err != nil {
		t.Fatal(err)
	}
	if hooks.List("me")[0].Secret != "" {
		t.Error("the list shows the secret")
	}

	log := func(person string) string {
		res, _, err := store.Write(person, "", "x", func(l *personLog) (Result, error) {
			return l.LogWorkout(LogWorkoutInput{Exercises: []ExerciseInput{{Name: "squat", Sets: []SetInput{{Reps: ptr(5)}}}}}, "api", "")
		})
		if err != nil {
			t.Fatal(err)
		}
		return res.(Logged).Workout.ID
	}
	id := log("me")
	log("someone else")
	if _, _, err := store.Write("me", "", "y", func(l *personLog) (Result, error) { return l.Update(UpdateInput{EntryID: id, Title: ptr("x")}) }); err != nil {
		t.Fatal(err) // workout.updated: not subscribed
	}
	if _, _, err := store.Write("me", "", "z", func(l *personLog) (Result, error) { return l.Delete(id) }); err != nil {
		t.Fatal(err)
	}
	hooks.Wait()

	if len(got) != 2 {
		t.Fatalf("%d deliveries, want created and deleted", len(got))
	}
	events := map[string]bool{}
	for i, r := range got {
		events[r.Header.Get("OWS-Event")] = true
		header := r.Header.Get("OWS-Signature")
		ts, _, _ := strings.Cut(strings.TrimPrefix(header, "t="), ",")
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			t.Fatalf("signature header %q", header)
		}
		if want := sign(sub.Secret, time.Unix(sec, 0), bodies[i]); header != want {
			t.Errorf("signature %s doesn't match the body with the secret", header)
		}
		var body map[string]any
		_ = json.Unmarshal(bodies[i], &body)
		if body["workout_id"] != id || body["subscription_id"] != sub.ID || body["delivery_id"] != r.Header.Get("OWS-Delivery") {
			t.Errorf("body %v", body)
		}
	}
	if !events["workout.created"] || !events["workout.deleted"] {
		t.Errorf("events %v", events)
	}
}
