package setup

import (
	"strings"
	"testing"
)

func TestSpecs(t *testing.T) {
	specs, err := Specs()
	if err != nil {
		t.Fatalf("Specs: %v", err)
	}
	// Sorted and derived from the [tools] table: "name = ver" → "name@ver"; an inline
	// table carries its tool options in mise's CLI form, "name[k=v]@ver".
	want := []string{"difftastic@latest", "github:dandavison/delta[matching=musl]@0.18.2"}
	if strings.Join(specs, ",") != strings.Join(want, ",") {
		t.Errorf("specs = %v, want %v", specs, want)
	}
}

func TestManifestNonEmpty(t *testing.T) {
	if !strings.Contains(Manifest(), "[tools]") {
		t.Error("embedded manifest missing a [tools] table")
	}
}
