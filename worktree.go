package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Worktree struct {
	Ticket string
	Path   string
	Branch string
	Repos  []string // submodules that are checked out in this worktree
}

// listWorktrees returns the superrepo's worktrees keyed by dir name, leaving out main.
func listWorktrees(cfg Config) map[string]Worktree {
	res := map[string]Worktree{}
	out, err := exec.Command("git", "-C", cfg.MainDir(), "worktree", "list", "--porcelain").Output()
	if err != nil {
		return res
	}
	mainReal, _ := filepath.EvalSymlinks(cfg.MainDir())
	for _, block := range strings.Split(strings.TrimSpace(string(out)), "\n\n") {
		var wt Worktree
		bare := false
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				wt.Path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "branch "):
				wt.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			case line == "bare":
				bare = true
			case line == "detached":
				wt.Branch = "(detached)"
			}
		}
		if bare || wt.Path == "" {
			continue
		}
		if real, _ := filepath.EvalSymlinks(wt.Path); real == mainReal {
			continue
		}
		wt.Ticket = filepath.Base(wt.Path)
		for _, sub := range submodulePaths(wt.Path) {
			if _, err := os.Stat(filepath.Join(wt.Path, sub, ".git")); err == nil {
				wt.Repos = append(wt.Repos, sub)
			}
		}
		res[wt.Ticket] = wt
	}
	return res
}

var gitmodulesPath = regexp.MustCompile(`^\s*path\s*=\s*(.+?)\s*$`)

func submodulePaths(dir string) []string {
	f, err := os.Open(filepath.Join(dir, ".gitmodules"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var paths []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := gitmodulesPath.FindStringSubmatch(sc.Text()); m != nil {
			paths = append(paths, m[1])
		}
	}
	return paths
}
