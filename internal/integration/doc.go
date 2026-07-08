// Package integration holds build-tagged, black-box tests that drive the mu binary
// against the local sandbox container (an openSUSE Leap sshd box that stands in for the
// real HPC login node — see internal/integration/sandbox). They are excluded from the
// default build and test; run them explicitly with the box up:
//
//	cd internal/integration/sandbox && docker compose up -d
//	go test -tags sandbox ./internal/integration/ -v
//
// The config defaults to the in-repo sandbox/test-config.toml (MU_SANDBOX_CONFIG overrides).
// Every test skips cleanly when the box is unreachable, so they never fail a normal
// `go test ./...` run (the tag already excludes them).
package integration
