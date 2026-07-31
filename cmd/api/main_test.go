package main

import (
	"testing"
)

// TestNewCodeLength verifies the code is always the expected length.
func TestNewCodeLength(t *testing.T) {
	for i := 0; i < 1000; i++ {
		code, err := newCode()
		if err != nil {
			t.Fatalf("newCode() error: %v", err)
		}
		if len(code) != shortCodeLen {
			t.Fatalf("code length = %d, want %d (code=%q)", len(code), shortCodeLen, code)
		}
	}
}

// TestNewCodeAlphabet verifies every character is in the allowed set.
func TestNewCodeAlphabet(t *testing.T) {
	allowed := make(map[byte]bool)
	for i := 0; i < len(codeAlphabet); i++ {
		allowed[codeAlphabet[i]] = true
	}
	for i := 0; i < 1000; i++ {
		code, err := newCode()
		if err != nil {
			t.Fatalf("newCode() error: %v", err)
		}
		for j := 0; j < len(code); j++ {
			if !allowed[code[j]] {
				t.Fatalf("code %q contains invalid char %q", code, string(code[j]))
			}
		}
	}
}

// TestNewCodeUniqueness verifies 10k generated codes are all unique.
// This catches a broken or biased RNG.
func TestNewCodeUniqueness(t *testing.T) {
	const n = 10_000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		code, err := newCode()
		if err != nil {
			t.Fatalf("newCode() error: %v", err)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q at iteration %d", code, i)
		}
		seen[code] = true
	}
}
