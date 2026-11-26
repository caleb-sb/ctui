package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// WorkType is one kind of work (bug, feat, review): its colour, its stages, and what Claude
// does once it has picked the type.
type WorkType struct {
	Colour       string   `toml:"colour"`       // hex, e.g. "#ff875f"
	Stages       []string `toml:"stages"`       // in order; "approve" means waiting for your approval of the stage before it
	Instructions string   `toml:"instructions"` // printed to Claude by `ctui type`; same placeholders as the prompts
}

const approveStage = "approve"

func defaultTypes() map[string]WorkType {
	return map[string]WorkType{
		"feat": {
			Colour: "#5fafff",
			Stages: []string{"start", "investigate", "spec", "approve", "tests", "approve", "implement", "approve", "checks", "mr", "demo", "finish"},
			Instructions: `Use scripts/start-feature to create the {ticket} worktree with the submodules the change touches. ` +
				`Investigate the issue, then write a TDD implementation spec and wait for my approval. ` +
				`Write the tests first and show me they fail, and wait for my approval. Then implement, ` +
				`show the tests pass and wait for my approval. Then run the checks, open the MR and make the demo.`,
		},
		"bug": {
			Colour:       "#ff875f",
			Stages:       []string{"start", "repro", "verify", "spec", "approve", "tests", "approve", "fix", "approve", "checks", "mr", "demo", "finish"},
			Instructions: `Use the fix-bug skill for {ticket}.`,
		},
		"review": {
			Colour:       "#d787d7",
			Stages:       []string{"start", "plan", "review", "finish"},
			Instructions: `Use the review-mr skill for {ticket}.`,
		},
	}
}

// typeNames returns the configured work types, sorted.
func (c Config) typeNames() []string {
	var names []string
	for n := range c.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (c Config) workType(name string) (WorkType, bool) {
	t, ok := c.Types[strings.ToLower(name)]
	return t, ok && len(t.Stages) > 0
}

// moveStage works out the new stage when Claude reports that it's entering stage name.
// Reporting the current stage again changes nothing. Otherwise the next stage with that name
// after the current one wins, and the stages jumped over are marked skipped. If there's none
// ahead, the nearest one behind wins (going back to redo something) and later skips are cleared.
func moveStage(stages []string, cur int, skipped []int, name string) (int, []int, error) {
	if cur >= 0 && cur < len(stages) && stages[cur] == name {
		return cur, skipped, nil
	}
	idx := -1
	for i := cur + 1; i < len(stages); i++ {
		if stages[i] == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i := min(cur, len(stages)-1); i >= 0; i-- {
			if stages[i] == name {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return cur, skipped, fmt.Errorf("unknown stage %q (stages: %s)", name, strings.Join(uniq(stages), ", "))
	}
	var keep []int
	for _, s := range skipped {
		if s < idx {
			keep = append(keep, s)
		}
	}
	for i := cur + 1; i < idx; i++ {
		keep = append(keep, i)
	}
	return idx, keep, nil
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// stageGuide is what Claude is told about reporting stages, by `ctui type` and after /clear.
func (c Config) stageGuide(ticket string, wt WorkType, cur int) string {
	ctui := shellQuote(selfPath())
	at := ""
	if cur >= 0 && cur < len(wt.Stages) {
		at = wt.Stages[cur]
		if at == approveStage && cur > 0 {
			at += " (waiting for my approval of " + wt.Stages[cur-1] + ")"
		}
	}
	return fmt.Sprintf(`The stages are: %s
As you move into each stage, run: %s stage %s <stage>
"approve" means you've finished the stage before it and are waiting for my approval; report it when you stop to ask. `+
		`Skipping a stage is fine: report the one you're moving to and the ones in between are marked skipped. `+
		`Reporting an earlier stage goes back to it.
Current stage: %s`, strings.Join(wt.Stages, " › "), ctui, ticket, at)
}

// runType is `ctui type TICKET TYPE`: Claude sets the kind of work, and learns its stages.
func runType(args []string) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 1
	}
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: ctui type TICKET %s\n", strings.Join(cfg.typeNames(), "|"))
		return 2
	}
	ticket, err := cfg.NormalizeTicket(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 2
	}
	name := strings.ToLower(args[1])
	wt, ok := cfg.workType(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "ctui: unknown work type %q (types: %s)\n", args[1], strings.Join(cfg.typeNames(), ", "))
		return 2
	}
	if _, err := updateState(ticket, func(s *State) { s.Type, s.Stage, s.Skipped = name, 0, nil }); err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 1
	}
	fmt.Printf("%s is %s work.\n%s\n", ticket, strings.ToUpper(name), cfg.stageGuide(ticket, wt, 0))
	if wt.Instructions != "" {
		fmt.Printf("\nWhat to do next: %s\n", cfg.Prompt(wt.Instructions, ticket))
	}
	return 0
}

// runStage is `ctui stage TICKET STAGE`.
func runStage(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ctui stage TICKET STAGE")
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 1
	}
	ticket, err := cfg.NormalizeTicket(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 2
	}
	name := strings.ToLower(strings.TrimSpace(args[1]))
	var wt WorkType
	var moveErr error
	st, err := updateState(ticket, func(s *State) {
		var ok bool
		if wt, ok = cfg.workType(s.Type); !ok {
			moveErr = fmt.Errorf("%s has no work type yet; run ctui type %s %s first", ticket, ticket, strings.Join(cfg.typeNames(), "|"))
			return
		}
		s.Stage, s.Skipped, moveErr = moveStage(wt.Stages, s.Stage, s.Skipped, name)
	})
	if err == nil {
		err = moveErr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 1
	}
	msg := fmt.Sprintf("%s stage: %s", ticket, wt.Stages[st.Stage])
	if len(st.Skipped) > 0 {
		var sk []string
		for _, i := range st.Skipped {
			sk = append(sk, wt.Stages[i])
		}
		msg += " (skipped: " + strings.Join(sk, ", ") + ")"
	}
	fmt.Println(msg)
	return 0
}
