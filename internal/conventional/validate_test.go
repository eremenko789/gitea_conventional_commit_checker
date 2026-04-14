package conventional

import (
	"strings"
	"testing"
)

func defaultTypes() map[string]struct{} {
	m := make(map[string]struct{})
	for _, t := range strings.Split("feat fix docs style refactor perf test build ci chore revert", " ") {
		m[t] = struct{}{}
	}
	return m
}

func TestValidateSubject_ok(t *testing.T) {
	allowed := defaultTypes()
	cases := []string{
		"feat: add thing",
		"fix(auth): handle nil user",
		"chore!: breaking cleanup",
		"feat(api)!: change endpoint",
		"docs: update README",
	}
	for _, s := range cases {
		r := ValidateSubject(s, allowed, false)
		if !r.OK {
			t.Fatalf("%q: want ok, got %s", s, r.Reason)
		}
	}
}

func TestValidateSubject_fail(t *testing.T) {
	allowed := defaultTypes()
	cases := []string{
		"not a conventional commit",
		"feat:",
		"feat: ",
		"unknown: hello world",
	}
	for _, s := range cases {
		r := ValidateSubject(s, allowed, false)
		if r.OK {
			t.Fatalf("%q: want fail", s)
		}
	}
}

func TestValidateSubject_mergeSkipped(t *testing.T) {
	allowed := defaultTypes()
	r := ValidateSubject("Merge branch 'x' into y", allowed, true)
	if !r.OK {
		t.Fatal(r.Reason)
	}
	r = ValidateSubject("Merge branch 'x' into y", allowed, false)
	if r.OK {
		t.Fatal("expected failure when merge not skipped")
	}
}

func TestValidateSubject_multipleInvalid(t *testing.T) {
	allowed := defaultTypes()
	msgs := []string{
		"bad one",
		"feat: good",
		"another bad",
	}
	var bad []string
	for _, m := range msgs {
		if !ValidateSubject(m, allowed, false).OK {
			bad = append(bad, m)
		}
	}
	if len(bad) != 2 {
		t.Fatalf("bad count: %v", bad)
	}
}
