package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

func TestWalltimeField(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", true},
		{"1:00:00", true},
		{"168:00:00", true},
		{"0:30:00", true},
		{"12:60:00", false},
		{"12:00", false},
		{"12h", false},
		{"::", false},
	}
	for _, c := range cases {
		if got := walltimeField(c.in, nil) == ""; got != c.ok {
			t.Errorf("walltimeField(%q) ok=%v, want %v", c.in, got, c.ok)
		}
	}
	if wallSeconds("2:30:15") != 2*3600+30*60+15 {
		t.Errorf("wallSeconds = %d", wallSeconds("2:30:15"))
	}
}

func TestIntField(t *testing.T) {
	for in, ok := range map[string]bool{"": true, "4": true, "0": false, "-2": false, "four": false} {
		if got := intField(in, nil) == ""; got != ok {
			t.Errorf("intField(%q) ok=%v, want %v", in, got, ok)
		}
	}
}

// TestQueueSeed locks the queue-field seeding shared by the sub/tunnel/shell forms: config
// default for a bare sub form, the literal for -q, config entry (or pending) for class
// flags, options deduped with the sentinel first.
func TestQueueSeed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[[cluster]]
name = "alpha"
domain = "a.example.mil"
nodes = ["hpc1"]
submit_queue = { default = "standard", gpu = "gpu_short" }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	config.ResetForTest()
	defer config.ResetForTest()

	// bare → config default selected; options = sentinel + configured entries
	val, pending, opts := queueSeed("alpha", &queueSel{}, true)
	if val != "standard" || pending != "" {
		t.Errorf("bare seed = %q pending %q", val, pending)
	}
	if got := strings.Join(opts, ","); got != schedDefault+",standard,gpu_short" {
		t.Errorf("options = %q", got)
	}

	// -q literal wins and joins the options
	if val, _, opts = queueSeed("alpha", &queueSel{queue: "special"}, true); val != "special" || !strings.Contains(strings.Join(opts, ","), "special") {
		t.Errorf("-q seed = %q opts %v", val, opts)
	}

	// class flag with a config entry resolves; debug falls to its literal; vis stays pending
	if val, pending, _ = queueSeed("alpha", &queueSel{gpu: true}, true); val != "gpu_short" || pending != "" {
		t.Errorf("gpu seed = %q pending %q", val, pending)
	}
	if val, pending, _ = queueSeed("alpha", &queueSel{debug: true}, true); val != "debug" || pending != "" {
		t.Errorf("debug seed = %q pending %q", val, pending)
	}
	if val, pending, _ = queueSeed("alpha", &queueSel{vis: true}, true); val != schedDefault || pending != "vis" {
		t.Errorf("vis seed = %q pending %q", val, pending)
	}

	// tunnel/shell pass bareDefault=false: a flagless form starts on the scheduler default,
	// NOT submit_queue.default — that entry is where batch work goes. A flag still resolves.
	if val, _, opts = queueSeed("alpha", &queueSel{}, false); val != schedDefault {
		t.Errorf("bare interactive seed = %q, want the scheduler default", val)
	}
	if got := strings.Join(opts, ","); got != schedDefault+",standard,gpu_short" {
		t.Errorf("interactive options = %q, want the configured queues offered anyway", got)
	}
	if val, _, _ = queueSeed("alpha", &queueSel{gpu: true}, false); val != "gpu_short" {
		t.Errorf("gpu interactive seed = %q", val)
	}
}

// TestEitherScriptOrJob pins the tunnel form's cross-field rule: submit a script or adopt a
// job, never both, never neither — the check RunE does for the flag path.
func TestEitherScriptOrJob(t *testing.T) {
	vals := func(script, job string) []string {
		return []string{tfScript: script, tfJob: job, tfQueue: "", tfAccount: "", tfPort: "", tfLocal: ""}
	}
	if msg := eitherScriptOrJob("", vals("serve.sh", "")); msg != "" {
		t.Errorf("script alone rejected: %s", msg)
	}
	if msg := eitherScriptOrJob("", vals("", "4501")); msg != "" {
		t.Errorf("job alone rejected: %s", msg)
	}
	if msg := eitherScriptOrJob("", vals("serve.sh", "4501")); msg == "" {
		t.Error("script AND job accepted")
	}
	if msg := eitherScriptOrJob("", vals("", "")); msg == "" {
		t.Error("neither script nor job accepted")
	}
}
