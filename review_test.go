package main

import (
	"bytes"
	"compress/gzip"
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

func TestUnpackTuicr(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("#!/bin/sh\necho bundled\n"))
	zw.Close()
	old := bundledTuicr
	bundledTuicr = gz.Bytes()
	defer func() { bundledTuicr = old }()

	p, bundled, err := tuicrPath(Config{})
	if err != nil || !bundled {
		t.Fatalf("tuicrPath: %q %v %v", p, bundled, err)
	}
	out, err := exec.Command(p).Output()
	if err != nil || string(out) != "bundled\n" {
		t.Fatalf("running unpacked tuicr: %q %v", out, err)
	}
	if p2, _, _ := tuicrPath(Config{}); p2 != p {
		t.Errorf("second call unpacked to %q, want %q", p2, p)
	}
	if _, bundled, _ := tuicrPath(Config{TuicrCmd: "sh"}); bundled {
		t.Error("tuicr_cmd should win over the bundled copy")
	}
}
