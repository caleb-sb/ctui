package main

import (
	"os"
	"reflect"
	"testing"
)

func TestExampleConfigMatchesDefaults(t *testing.T) {
	t.Setenv("CTUI_CONFIG", "config.example.toml")
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := defaultConfig()
	want.Root = expandHome(want.Root)
	got.ClaudeArgs = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("example config differs from defaults:\n got %+v\nwant %+v", got, want)
	}
}

func TestNormalizeTicket(t *testing.T) {
	c := defaultConfig()
	for in, want := range map[string]string{
		"5170": "PROJ-5170", "proj-5170": "PROJ-5170", " PROJ-1 ": "PROJ-1",
		"Login-Mockups": "login-mockups", "docs_site": "docs_site",
	} {
		if got, err := c.NormalizeTicket(in); err != nil || got != want {
			t.Errorf("NormalizeTicket(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := c.NormalizeTicket("PROJ 5170; rm -rf"); err == nil {
		t.Error("expected error for unsafe ticket")
	}
	if _, err := c.NormalizeTicket("v2-1-docs"); err == nil {
		t.Error("expected error for a name that starts like a ticket")
	}
}

func TestTicketFromDir(t *testing.T) {
	for in, want := range map[string]string{
		"PROJ-5166-fix-rounding": "PROJ-5166",
		"PROJ-5166":              "PROJ-5166",
		"proj-12-x":              "PROJ-12",
		"AB_C-7-a-b-9":           "AB_C-7",
		"scratch":                "scratch",
		"PROJ-x":                 "proj-x",
		"Login-Mockups":          "login-mockups",
	} {
		if got := ticketFromDir(in); got != want {
			t.Errorf("ticketFromDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMoveStage(t *testing.T) {
	st := defaultTypes()["feat"].Stages // start investigate spec approve tests approve implement approve checks mr demo finish
	cases := []struct {
		cur     int
		skipped []int
		name    string
		want    int
		wantSk  []int
	}{
		{0, nil, "investigate", 1, nil},
		{0, nil, "spec", 2, []int{1}},          // jumped over investigate
		{2, []int{1}, "approve", 3, []int{1}},  // the approve after spec
		{3, []int{1}, "approve", 3, []int{1}},  // reporting it again stays put
		{4, nil, "approve", 5, nil},            // the approve after tests, not after spec
		{6, nil, "checks", 8, []int{7}},        // skipped the implement approval
		{8, []int{3, 7}, "tests", 4, []int{3}}, // back to redo tests: later skips cleared
		{11, nil, "finish", 11, nil},
	}
	for _, c := range cases {
		got, sk, err := moveStage(st, c.cur, c.skipped, c.name)
		if err != nil || got != c.want || !reflect.DeepEqual(sk, c.wantSk) {
			t.Errorf("moveStage(cur %d, %v, %q) = %d, %v, %v; want %d, %v", c.cur, c.skipped, c.name, got, sk, err, c.want, c.wantSk)
		}
	}
	if _, _, err := moveStage(st, 0, nil, "deploy"); err == nil {
		t.Error("expected error for unknown stage")
	}
}

func TestTypeOverride(t *testing.T) {
	f := t.TempDir() + "/c.toml"
	if err := os.WriteFile(f, []byte("[types.review]\nstages = [\"start\", \"finish\"]\n[types.spike]\nstages = [\"start\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CTUI_CONFIG", f)
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Types["review"]; len(got.Stages) != 2 || got.Colour != "" {
		t.Errorf("review should be replaced, got %+v", got)
	}
	if _, ok := c.workType("bug"); !ok {
		t.Error("default bug type should stay")
	}
	if _, ok := c.workType("spike"); !ok {
		t.Error("new type should be added")
	}
}

func TestStagelessType(t *testing.T) {
	t.Setenv("CTUI_CONFIG", t.TempDir()+"/none.toml")
	t.Setenv("CTUI_STATE_DIR", t.TempDir())
	if code := runType([]string{"login-mockups", "explore"}); code != 0 {
		t.Fatalf("ctui type explore exited %d", code)
	}
	if code := runStage([]string{"login-mockups", "finish"}); code != 0 {
		t.Errorf("ctui stage on a stageless type exited %d, want 0", code)
	}
	if st := readState("login-mockups"); st.Type != "explore" || st.Stage != 0 {
		t.Errorf("state = %+v", st)
	}
}
