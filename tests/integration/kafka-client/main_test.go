package main

import (
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRecordMatchesOptionalExpectedKey(t *testing.T) {
	t.Parallel()
	record := &kgo.Record{Key: []byte("integration-lab4"), Value: []byte("only-lab4")}
	for _, test := range []struct {
		name        string
		message     string
		expectedKey string
		want        bool
	}{
		{name: "matching key and value", message: "only-lab4", expectedKey: "integration-lab4", want: true},
		{name: "wrong key", message: "only-lab4", expectedKey: "another", want: false},
		{name: "wrong value", message: "other", expectedKey: "integration-lab4", want: false},
		{name: "intentional value-only mode", message: "only-lab4", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := recordMatches(record, test.message, test.expectedKey); got != test.want {
				t.Fatalf("recordMatches() = %t, want %t", got, test.want)
			}
		})
	}
}
