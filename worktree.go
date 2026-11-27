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
	Review bool     // branch.<branch>.review is true: start-feature made it as a review tree
}

// ticketKey matches the ticket at the front of a worktree dir name such as "PROJ-5166-fix-rounding".
var ticketKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+`)

// isTicket reports whether s is a whole ticket key such as "PROJ-5166", rather than an exploration name.
func isTicket(s string) bool { return s != "" && ticketKey.FindString(s) == s }

// ticketFromDir returns the ticket a worktree dir belongs to: "PROJ-5166-fix-rounding" gives
// "PROJ-5166". A name that doesn't start with a ticket is an exploration, named by the whole dir
// in lowercase.
func ticketFromDir(name string) string {
	if m := ticketKey.FindString(name); m != "" {
		return strings.ToUpper(m)
	}
	return strings.ToLower(name)
}

// listWorktrees returns the superrepo's worktrees keyed by ticket, leaving out main.
// Worktree dirs are named <TICKET>-<slug>.
func listWorktrees(cfg Config) map[string]Worktree {
	res := map[string]Worktree{}
	out, err := exec.Command("git", "-C", cfg.MainDir(), "worktree", "list", "--porcelain").Output()
	if err != nil {
		return res
	}
	mainReal, _ := filepath.EvalSymlinks(cfg.MainDir())
	review := reviewBranches(cfg)
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
		wt.Ticket = ticketFromDir(filepath.Base(wt.Path))
		wt.Review = review[wt.Branch]
		for _, sub := range submodulePaths(wt.Path) {
			if _, err := os.Stat(filepath.Join(wt.Path, sub, ".git")); err == nil {
				wt.Repos = append(wt.Repos, sub)
			}
		}
		if _, dup := res[wt.Ticket]; dup {
			continue // keep the first worktree git lists for a ticket
		}
		res[wt.Ticket] = wt
	}
	return res
}

// reviewBranches returns the branches with branch.<name>.review set to true.
func reviewBranches(cfg Config) map[string]bool {
	res := map[string]bool{}
	out, err := exec.Command("git", "-C", cfg.MainDir(), "config", "--bool", "--get-regexp", `^branch\..*\.review$`).Output()
	if err != nil {
		return res
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, val, _ := strings.Cut(line, " ")
		if val == "true" {
			res[strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".review")] = true
		}
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
