package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunUpgrade_AlphaAndRC_MutuallyExclusive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runUpgrade([]string{"--alpha", "--rc"}, &stdout, &stderr, options{})
	if code != 2 {
		t.Fatalf("runUpgrade() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("stderr = %q, want it to explain the conflict", stderr.String())
	}
}

func TestRunUpgrade_UnknownFlag_Refuses(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runUpgrade([]string{"--bogus"}, &stdout, &stderr, options{})
	if code != 2 {
		t.Fatalf("runUpgrade() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr = %q, want it to contain Usage", stderr.String())
	}
}
