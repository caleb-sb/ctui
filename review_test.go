package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepoStatus(t *testing.T) {
	dir := t.TempDir()
	up, sub := filepath.Join(dir, "up"), filepath.Join(dir, "sub")
	run := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", d, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(up, 0o755); err != nil {
		t.Fatal(err)
	}
	run(up, "init", "-q", "-b", "main")
	run(up, "commit", "-q", "--allow-empty", "-m", "init")
	run(dir, "clone", "-q", up, sub)

	if got := repoStatus(sub).summary(); got != "no changes" {
		t.Errorf("fresh clone: %q", got)
	}
	run(sub, "checkout", "-q", "-b", "PROJ-1-x")
	run(sub, "commit", "-q", "--allow-empty", "-m", "a")
	run(sub, "commit", "-q", "--allow-empty", "-m", "b")
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ri := repoStatus(sub)
	if ri.Branch != "PROJ-1-x" || ri.Name != "sub" {
		t.Errorf("branch/name: %+v", ri)
	}
	if got := ri.summary(); got != "2 commits · 1 file dirty" {
		t.Errorf("summary: %q", got)
	}
}
