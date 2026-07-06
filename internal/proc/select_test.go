package proc

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		tok  string
		want Kind
	}{
		{"funwave", Mask},
		{"py.*", Mask},
		{"~4501", Mask}, // ~ forces mask for a numeric-that's-a-name
		{"4501", IDSingle},
		{"4501-4510", IDRange},
		{"4501,4507", IDList},
	}
	for _, c := range cases {
		if got := Classify(c.tok).Kind; got != c.want {
			t.Errorf("Classify(%q).Kind = %v, want %v", c.tok, got, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	ps := []Process{
		{PID: 100, Name: "funwave"},
		{PID: 101, Name: "python"},
		{PID: 4501, Name: "ww3"},
		{PID: 4505, Name: "ww3"},
	}
	if got := Classify("funwave").Match(ps); len(got) != 1 || got[0].PID != 100 {
		t.Errorf("mask funwave: %+v", got)
	}
	if got := Classify("101").Match(ps); len(got) != 1 || got[0].PID != 101 {
		t.Errorf("single 101: %+v", got)
	}
	if got := Classify("4500-4510").Match(ps); len(got) != 2 {
		t.Errorf("range 4500-4510: want 2, got %+v", got)
	}
	if got := Classify("100,4505").Match(ps); len(got) != 2 {
		t.Errorf("list 100,4505: want 2, got %+v", got)
	}
	// ~ forces mask even though the token is numeric
	numeric := []Process{{PID: 1, Name: "4501"}}
	if got := Classify("~4501").Match(numeric); len(got) != 1 {
		t.Errorf("~mask over numeric name: want 1, got %+v", got)
	}
}
