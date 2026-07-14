package cli

import (
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/tomledit"
)

const cfgSample = `hpc_user = "someuser"

[transfer]
rsync_opts = "-au"

[[cluster]]
name      = "dsrc1"
domain    = "dsrc1.example.hpc.mil"
scheduler = "pbs"
account   = "CLUSTER-ALLOC"

  [[cluster.node]]
  name    = "node-a"
  account = "NODE-ALLOC"

  [[cluster.node]]
  name = "node-b"
`

// find returns the leaf at a path in the built tree, so a test can assert on what the panel
// would actually show.
func findLeaf(t *testing.T, doc *tomledit.Doc, path ...string) (value, origin string) {
	t.Helper()
	root, _ := buildTree(doc)
	nodes := root
	for d, want := range path {
		for _, n := range nodes {
			// Section labels carry decoration ("[[cluster]] dsrc1"), leaves don't.
			if n.Label != want && !strings.HasSuffix(n.Label, " "+want) {
				continue
			}
			if d == len(path)-1 {
				if n.Field == nil {
					t.Fatalf("%v is a section, not a leaf", path)
				}
				return n.Field.Value, n.Origin
			}
			nodes = n.Children
			break
		}
	}
	t.Fatalf("no leaf at %v", path)
	return "", ""
}

// TestBuildTreeProvenance is the panel's reason to exist: a node's resolved value and the
// SCOPE it came from — invisible in the file, decisive for what a lookup returns.
func TestBuildTreeProvenance(t *testing.T) {
	doc := tomledit.Parse(cfgSample)

	// node-a overrides account → its own value, no note.
	if v, o := findLeaf(t, doc, "dsrc1", "node-a", "account"); v != "NODE-ALLOC" || o != "" {
		t.Errorf("node-a account = %q (%q), want its own override", v, o)
	}
	// node-b doesn't → it shows the CLUSTER's value, marked as inherited and attributed.
	wantOrigin := render.Glyph("↳ ", "< ") + "from dsrc1"
	if v, o := findLeaf(t, doc, "dsrc1", "node-b", "account"); v != "CLUSTER-ALLOC" || o != wantOrigin {
		t.Errorf("node-b account = %q (%q), want the cluster's, attributed", v, o)
	}
	// Neither scope sets cores_per_node.
	if v, o := findLeaf(t, doc, "dsrc1", "node-b", "cores_per_node"); v != "" || o != "unset" {
		t.Errorf("node-b cores_per_node = %q (%q), want unset", v, o)
	}
	// A table absent from the file is not offered at all (creating it is a hand-edit).
	root, _ := buildTree(doc)
	for _, n := range root {
		if n.Label == "[sshfs]" {
			t.Error("offered [sshfs], which the file doesn't have")
		}
	}
}

// TestApplyChanges walks the write-back path a save takes: a Change on an INHERITED value
// must write an override into the node's own block, not touch the cluster's line.
func TestApplyChanges(t *testing.T) {
	doc := tomledit.Parse(cfgSample)
	_, targets := buildTree(doc)

	apply := func(path []string, val string) {
		tgt, ok := targets[strings.Join(path, "\x00")]
		if !ok {
			t.Fatalf("no target for %v", path)
		}
		raw := val
		if tgt.quoted {
			raw = tomledit.Quote(val)
		}
		doc.Set(tgt.table, tgt.key, raw)
	}
	apply([]string{"dsrc1", "node-b", "account"}, "NEW-ALLOC")  // was inherited
	apply([]string{"dsrc1", "node-a", "cores_per_node"}, "192") // was absent
	apply([]string{"transfer", "rsync_opts"}, "-au --partial")  // plain replace
	out := doc.String()

	if !strings.Contains(out, "  name = \"node-b\"\n  account = \"NEW-ALLOC\"") {
		t.Errorf("the override didn't land in node-b's block:\n%s", out)
	}
	// The cluster's own account must be untouched — an override is not an edit of the default.
	if !strings.Contains(out, `account   = "CLUSTER-ALLOC"`) {
		t.Errorf("writing a node override rewrote the cluster's default:\n%s", out)
	}
	if !strings.Contains(out, "  cores_per_node = 192") { // bare int, not quoted
		t.Errorf("cores_per_node not written as a bare int:\n%s", out)
	}
	if !strings.Contains(out, `rsync_opts = "-au --partial"`) {
		t.Errorf("rsync_opts not replaced:\n%s", out)
	}
	if strings.Count(out, `name    = "node-a"`) != 1 {
		t.Errorf("the document was restructured:\n%s", out)
	}
}

func TestIntOrEmpty(t *testing.T) {
	for _, ok := range []string{"", "0", "128", " 192 "} {
		if msg := intOrEmpty(ok, nil); msg != "" {
			t.Errorf("intOrEmpty(%q) = %q, want accepted", ok, msg)
		}
	}
	for _, bad := range []string{"-1", "many", "1.5"} {
		if msg := intOrEmpty(bad, nil); msg == "" {
			t.Errorf("intOrEmpty(%q) accepted", bad)
		}
	}
}
