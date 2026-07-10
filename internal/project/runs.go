package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

// Run is one run.toml provenance record plus where it was found — the planted
// database row `mu project runs` tabulates (SQLite arrives later as a derived
// cache only, never the source of truth).
type Run struct {
	JobID     string `toml:"jobid" json:"jobid"`
	Scheduler string `toml:"scheduler" json:"scheduler,omitempty"`
	Started   string `toml:"started" json:"started,omitempty"`
	Cluster   string `toml:"cluster" json:"cluster,omitempty"`
	Queue     string `toml:"queue" json:"queue,omitempty"`
	Case      string `toml:"case" json:"case,omitempty"`
	Commit    string `toml:"commit" json:"commit,omitempty"`
	Dirty     bool   `toml:"dirty" json:"dirty"`
	Dir       string `toml:"-" json:"dir"` // run dir holding the record
}

// CollectRuns walks the trees for run.toml records, newest started first. The
// records travel with the runs, so whichever tiers exist HERE (project tree on
// $HOME, staging on $WORKDIR) is exactly what a split-HPC project can answer
// locally. Unreadable/garbled records are skipped — a half-written run.toml
// from a live prep must not kill the listing.
func CollectRuns(trees []string) []Run {
	var out []Run
	for _, tree := range trees {
		_ = filepath.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.IsDir() || d.Name() != "run.toml" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			var r Run
			if toml.Unmarshal(data, &r) != nil || r.JobID == "" {
				return nil
			}
			r.Dir = filepath.Dir(path)
			out = append(out, r)
			return nil
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out
}

// RunTrees are the local roots CollectRuns should walk for the project: the
// project tree itself plus its $WORKDIR staging mirror when it exists here.
func RunTrees(root string) []string {
	trees := []string{root}
	rel, err := HomeRel(root)
	if err != nil {
		return trees
	}
	if w := os.Getenv("WORKDIR"); w != "" {
		if stage := filepath.Join(w, rel); dirExists(stage) {
			trees = append(trees, stage)
		}
	}
	return trees
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
