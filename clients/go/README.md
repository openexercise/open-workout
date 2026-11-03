# lifterclub (Go)

A thin Go client for the [Lifterclub API](https://lifterclub.com/developers), on the standard
library alone: `net/http`, the access token on every request, an `Idempotency-Key` on every
POST, RFC 9457 problems as errors, and keyset paging over workouts and the change feed.
Workouts are [Open Workout Schema](https://openworkout.dev) documents.

```go
import lifterclub "github.com/openexercise/open-workout/clients/go"

c := lifterclub.New("", tokens.Access) // "" is https://api.lifterclub.com

reps, load := 5, 185.0
logged, err := c.LogWorkout(ctx, lifterclub.WorkoutInput{
	ExternalID: "my-42",
	Exercises:  []lifterclub.ExerciseInput{{Name: "bench press", Sets: []lifterclub.SetInput{{Reps: &reps, Load: &load, Unit: lifterclub.Lb}}}},
})
if lifterclub.HasCode(err, lifterclub.CodeRateLimited) {
	var e *lifterclub.Error
	errors.As(err, &e)
	time.Sleep(e.RetryAfter) // e.Detail says whether the account's or the app's limit was hit
}
fmt.Println(logged.Summary)

// Mirror a log: every change since last time, oldest first, deleted ones as tombstones.
for ch, err := range c.Changes(ctx, lifterclub.ChangesOptions{UpdatedSince: lastSeen, IncludeDeleted: true}) {
	if err != nil {
		return err
	}
	if ch.Deleted != nil {
		remove(ch.Deleted.ID)
	} else {
		store(*ch.Workout)
	}
	lastSeen = ch.UpdatedAt()
}

// A webhook delivery.
ok := lifterclub.VerifySignature(secret, r.Header.Get(lifterclub.SignatureHeader), body, time.Now(), 5*time.Minute)
```

Getting the token is the app's job: OAuth 2.1 with PKCE, as the API guide describes. Set
`TokenSource` to hand the client the current token as you refresh it. Pass
`lifterclub.WithIdempotencyKey(ctx, key)` to retry a write across restarts with the same key.
Routes without a method here go through `Do`.
