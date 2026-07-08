package cli

import "testing"

func TestToolName(t *testing.T) {
	cases := map[string]string{
		"github:dandavison/delta@0.18.2": "delta",
		"difftastic@latest":              "difftastic",
		"ripgrep":                        "ripgrep",
		"cargo:some/pkg@1.2.3":           "pkg",
	}
	for in, want := range cases {
		if got := toolName(in); got != want {
			t.Errorf("toolName(%q) = %q, want %q", in, got, want)
		}
	}
}
