# Go quickstart

[`clients/go`](../clients/go) is a thin client on the standard library. Getting the token is your
app's job (OAuth 2.1 with PKCE, found from the server's `401`); for trying it out, borrow one.

## A token

- **Template server:** its fixed token, `dev-token` (see [Where to run them](README.md#where-to-run-them)).
- **Sandbox:** sign in with the example app and print the token it gets:

  ```bash
  cd partner-app && npm install
  npm start -- https://lifterclub-api.runmighty.dev --print-token
  ```

  It lasts an hour.

## Run it

```bash
mkdir ows-go && cd ows-go
go mod init example.com/ows-go
go get github.com/openexercise/open-workout/clients/go
```

`main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	openworkout "github.com/openexercise/open-workout/clients/go"
)

func main() {
	ctx := context.Background()
	c := openworkout.New(os.Getenv("OWS_BASE_URL"), os.Getenv("OWS_TOKEN"))

	reps, load := 5, 100.0
	logged, err := c.LogWorkout(ctx, openworkout.WorkoutInput{
		ExternalID: fmt.Sprintf("go-quickstart-%d", time.Now().Unix()),
		Exercises: []openworkout.ExerciseInput{{
			Name: "back squat",
			Sets: []openworkout.SetInput{{Reps: &reps, Load: &load, Unit: openworkout.Kg}},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(logged.Summary)

	// Everything that changed in the last day, oldest first; deleted workouts as tombstones.
	for ch, err := range c.Changes(ctx, openworkout.ChangesOptions{UpdatedSince: time.Now().Add(-24 * time.Hour), IncludeDeleted: true}) {
		if err != nil {
			log.Fatal(err)
		}
		if ch.Deleted != nil {
			fmt.Println("deleted", ch.Deleted.ID)
		} else {
			fmt.Println("changed", ch.Workout.ID)
		}
	}
}
```

```bash
OWS_BASE_URL=https://lifterclub-api.runmighty.dev OWS_TOKEN=lca_… go run .
OWS_BASE_URL=http://localhost:8080 OWS_TOKEN=dev-token go run .
```

## Next

- Errors are `*openworkout.Error` with the problem's `Code`; `openworkout.HasCode(err,
  openworkout.CodeRateLimited)` and `RetryAfter` tell you when to try again.
- `openworkout.VerifySignature` checks a webhook delivery's `OWS-Signature`.
- To check a server you're building, run [`ows conformance`](../README.md#check-a-server); its
  `conformance` package also runs inside Go tests.
