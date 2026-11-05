# openworkout (Go)

A thin Go client for any [Open Workout](https://openworkout.dev) server, on the standard library
alone: `net/http`, the access token on every request, an `Idempotency-Key` on every POST, RFC 9457
problems as errors, and keyset paging over workouts and the change feed. Workouts are Open Workout
Schema documents, and exercises are [Open Exercise](https://openexercise.org) IDs.

It covers the API core (`api/core.yaml`), which every server serves. Methods marked "Lifterclub
extension" (`Me`, `LogText`, `Parse`, `Records`, `Progress`, `CreateExercise`, `LastSets`,
`Profile`, `UpdateProfile`) call routes that [Lifterclub](https://lifterclub.com/developers), the
reference implementation, serves beyond the core; another server may answer them with 404.

```go
import "github.com/openexercise/open-workout/clients/go"

c := openworkout.New(openworkout.LifterclubURL, tokens.Access) // or any Open Workout server

reps, load := 5, 185.0
logged, err := c.LogWorkout(ctx, openworkout.WorkoutInput{
	ExternalID: "my-42",
	Exercises:  []openworkout.ExerciseInput{{Name: "bench press", Sets: []openworkout.SetInput{{Reps: &reps, Load: &load, Unit: openworkout.Lb}}}},
})
if openworkout.HasCode(err, openworkout.CodeRateLimited) {
	var e *openworkout.Error
	errors.As(err, &e)
	time.Sleep(e.RetryAfter) // e.Detail says which limit was hit
}
fmt.Println(logged.Summary)

// Mirror a log: every change since last time, oldest first, deleted ones as tombstones.
for ch, err := range c.Changes(ctx, openworkout.ChangesOptions{UpdatedSince: lastSeen, IncludeDeleted: true}) {
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
ok := openworkout.VerifySignature(secret, r.Header.Get(openworkout.SignatureHeader), body, time.Now(), 5*time.Minute)
```

Getting the token is the app's job: OAuth 2.1 with PKCE, found from the server's 401. Set
`TokenSource` to hand the client the current token as you refresh it. Pass
`openworkout.WithIdempotencyKey(ctx, key)` to retry a write across restarts with the same key.
Routes without a method here go through `Do`.

Webhooks are signed in `OWS-Signature`. Lifterclub also sends the same value as
`Lifterclub-Signature` (`LifterclubSignatureHeader`) until OWS v1, for receivers written before.

Earlier versions were package `lifterclub` with a default server. Now the package is
`openworkout`, and `New` takes the server's address.
