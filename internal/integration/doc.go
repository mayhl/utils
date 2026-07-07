// Package integration holds build-tagged, black-box tests that drive the mu binary
// against the local sandbox container (an openSUSE Leap sshd box that stands in for the
// HPE Cray login node — see ~/.config/sandbox). They are excluded from the default
// build and test; run them explicitly with the box up:
//
//	cd ~/.config/sandbox && docker compose up -d
//	MU_SANDBOX_CONFIG=~/.config/sandbox/test-config.toml \
//	  go test -tags sandbox ./internal/integration/ -v
//
// Every test skips cleanly when MU_SANDBOX_CONFIG is unset or the box is unreachable,
// so they never fail a normal `go test ./...` run (the tag already excludes them).
package integration
