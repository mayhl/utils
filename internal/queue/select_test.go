package queue

import "testing"

func sampleJobs() []Job {
	return []Job{
		{ID: "1284570.hpc1", ShortID: "1284570", Name: "funwave", User: "me"},
		{ID: "1284571.hpc1", ShortID: "1284571", Name: "ww3", User: "me"},
		{ID: "9001", ShortID: "9001", Name: "python", User: "me"}, // SLURM-style bare id
	}
}

func ids(js []Job) []string {
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = j.ID
	}
	return out
}

func TestMatchShortIDResolvesToFull(t *testing.T) {
	js := sampleJobs()
	got := Match(js, "1284570", false)
	if len(got) != 1 || got[0].ID != "1284570.hpc1" {
		t.Fatalf("short id 1284570 should resolve to full 1284570.hpc1, got %v", ids(got))
	}
	// full native id also matches
	if got := Match(js, "1284570.hpc1", false); len(got) != 1 || got[0].ID != "1284570.hpc1" {
		t.Errorf("full id: got %v", ids(got))
	}
}

func TestMatchRangeAndList(t *testing.T) {
	js := sampleJobs()
	if got := Match(js, "1284570-1284571", false); len(got) != 2 {
		t.Errorf("range: want 2, got %v", ids(got))
	}
	if got := Match(js, "1284570,9001", false); len(got) != 2 {
		t.Errorf("list: want 2, got %v", ids(got))
	}
}

func TestMatchNameMaskAndForce(t *testing.T) {
	js := sampleJobs()
	if got := Match(js, "ww3", false); len(got) != 1 || got[0].Name != "ww3" {
		t.Errorf("name mask ww3: got %v", ids(got))
	}
	// ~ forces a name mask: "9001" as a name matches nothing (it's an id, not a name)
	if got := Match(js, "~9001", false); len(got) != 0 {
		t.Errorf("~9001 should be a name mask matching no Name, got %v", ids(got))
	}
}

func TestMatchAllDedups(t *testing.T) {
	js := sampleJobs()
	// both tokens hit the same job (1284570 is funwave's short id) → one result
	if got := MatchAll(js, []string{"1284570", "funwave"}, false); len(got) != 1 {
		t.Errorf("dedup: want 1, got %v", ids(got))
	}
	if got := MatchAll(js, []string{"1284571", "funwave"}, false); len(got) != 2 {
		t.Errorf("union: want 2, got %v", ids(got))
	}
}
