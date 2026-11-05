package conformance

import (
	"testing"
	"time"
)

func TestSignatureVectors(t *testing.T) {
	cases, err := SignatureVectors()
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		at := time.Unix(c.Timestamp, 0)
		if got := Sign(c.Secret, at, []byte(c.Body)); got != c.Signature {
			t.Errorf("%s: Sign = %s, want %s", c.Name, got, c.Signature)
		}
		if err := VerifySignature(c.Secret, c.Signature, []byte(c.Body), at.Add(time.Minute), 5*time.Minute); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
		if err := VerifySignature(c.Secret, c.Signature, []byte(c.Body+" "), at, 5*time.Minute); err == nil {
			t.Errorf("%s: a changed body verified", c.Name)
		}
		if err := VerifySignature(c.Secret+"x", c.Signature, []byte(c.Body), at, 5*time.Minute); err == nil {
			t.Errorf("%s: another secret verified", c.Name)
		}
		if err := VerifySignature(c.Secret, c.Signature, []byte(c.Body), at.Add(10*time.Minute), 5*time.Minute); err == nil {
			t.Errorf("%s: a stale delivery verified", c.Name)
		}
	}
	if err := VerifySignature("s", "v1=abc", nil, time.Now(), time.Minute); err == nil {
		t.Error("a header without t verified")
	}
}
