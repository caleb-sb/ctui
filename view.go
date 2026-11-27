package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

var (
	cAccent = lipgloss.Color("141") // soft purple
	cSelBg  = lipgloss.Color("236")

	stWaiting = fg("203").Bold(true)
	stDone    = fg("221").Bold(true)
	stWorking = fg("114")
	stDim     = fg("243")
	stFaint   = fg("239")
	stLabel   = fg("245")
	stBorder  = fg("240")
	stFuture  = fg("240")
	pulseBg   = "#262626" // what the approval diamond fades towards
	stTitle   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stKey     = lipgloss.NewStyle().Foreground(cAccent)
	stErr     = fg("203")
	stOK      = fg("114")
	spinner   = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// panel draws a rounded box exactly w wide and h tall. title sits on the left of the top
// border and right on its right side. Body lines are truncated and padded to fit.
func panel(title, right string, body []string, w, h int) string {
	if w < 10 || h < 2 {
		return ""
	}
	inner := w - 4 // "│ " + content + " │"

	rightPart := ""
	if right != "" {
		rightPart = " " + right + " "
	}
	// ╭─ title ─────── right ─╮
	fill := w - 6 - lipgloss.Width(title) - lipgloss.Width(rightPart)
	if fill < 1 && rightPart != "" {
		rightPart = ""
		fill = w - 6 - lipgloss.Width(title)
	}
	if fill < 1 {
		title = ansi.Truncate(title, max(0, w-7), "…")
		fill = w - 6 - lipgloss.Width(title)
	}
	var b strings.Builder
	b.WriteString(stBorder.Render("╭─ ") + title + stBorder.Render(" "+strings.Repeat("─", max(0, fill))) +
		rightPart + stBorder.Render("─╮") + "\n")

	side := stBorder.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = ansi.Truncate(body[i], inner, "…")
		}
		pad := max(0, inner-ansi.StringWidth(line))
		b.WriteString(side + " " + line + "\x1b[0m" + strings.Repeat(" ", pad) + " " + side + "\n")
	}
	b.WriteString(stBorder.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.mode == mHelp {
		return m.helpView()
	}
	w, avail := m.w, m.h-1 // last line is the footer
	inner := w - 4

	// heights: list grows with the number of tickets (up to a third of the screen),
	// details is fixed, and the preview gets the rest
	const detailsH = 5
	listBody := min(max(1, len(m.rows)), max(1, avail/3-2))
	if m.mode == mReview {
		listBody = min(max(1, len(m.repos)), max(1, avail/3-2))
	}
	listH := listBody + 2
	previewH := avail - listH - detailsH
	if previewH < 3 {
		previewH = 0
	}

	// scroll the list to keep the cursor visible
	if m.cur < m.offset {
		m.offset = m.cur
	}
	if m.cur >= m.offset+listBody {
		m.offset = m.cur - listBody + 1
	}

	// ── list ──
	var list []string
	if len(m.rows) == 0 {
		list = append(list, stDim.Render("no sessions or worktrees yet. Press ")+stKey.Render("n")+stDim.Render(" to start one."))
	}
	typeW, ticketW := 0, 0
	for _, r := range m.rows {
		typeW, ticketW = max(typeW, len(typeLabel(r))), max(ticketW, len(r.Ticket))
	}
	for i := m.offset; i < min(len(m.rows), m.offset+listBody); i++ {
		list = append(list, m.rowLine(m.rows[i], i == m.cur, inner, typeW, ticketW))
	}
	out := []string{panel(stTitle.Render("ctui")+stDim.Render("  "+tildify(m.cfg.Root)), m.counts(), list, w, listH)}
	r, ok := m.selected()
	if m.mode == mReview {
		r, ok = m.reviewRow()
		out = []string{m.pickerPanel(r, w, listH)}
	}

	// ── details ──
	if ok {
		title := r.Ticket
		if r.WT != nil {
			title = filepath.Base(r.WT.Path)
		}
		right := ""
		if r.Type != "" {
			right = typeStyle(r).Bold(true).Render(typeLabel(r))
		}
		out = append(out, panel(stTitle.Render(title), right, m.details(r), w, detailsH))
	} else {
		out = append(out, panel(stDim.Render("details"), "", nil, w, detailsH))
	}

	// ── preview ──
	if previewH > 0 {
		var lines []string
		switch {
		case ok && m.preview != "":
			lines = strings.Split(m.preview, "\n")
			if n := previewH - 2; len(lines) > n {
				lines = lines[len(lines)-n:]
			}
		case ok && r.St.SessionID != "":
			lines = []string{stDim.Render("no Claude running. Press ") + stKey.Render("enter") + stDim.Render(" to resume the conversation.")}
		case ok:
			lines = []string{stDim.Render("no Claude running")}
		}
		out = append(out, panel(stDim.Render("session"), "", lines, w, previewH))
	}

	return strings.Join(out, "\n") + "\n" + m.footer()
}

// rowLine renders a list entry: selection bar, status icon, work type, ticket, stage pipeline.
// typeW and ticketW are the column widths, so the pipelines line up.
func (m *model) rowLine(r row, sel bool, inner, typeW, ticketW int) string {
	bg := func(s lipgloss.Style) lipgloss.Style {
		if sel {
			return s.Background(cSelBg)
		}
		return s
	}
	plain := bg(lipgloss.NewStyle())
	icon, ist := m.icon(r.Status)
	bar := " "
	if sel {
		bar = bg(stTitle).Render("▌")
	}
	line := bar + plain.Render(" ") + bg(ist).Render(icon) + plain.Render(" ")
	if typeW > 0 {
		line += bg(typeStyle(r).Bold(true)).Render(fmt.Sprintf("%-*s", typeW, typeLabel(r))) + plain.Render(" ")
	}
	line += bg(lipgloss.NewStyle().Bold(sel)).Render(fmt.Sprintf("%-*s", ticketW, r.Ticket))
	if r.hasStage() {
		line += plain.Render("  ") + m.pipeline(r, bg, inner-ansi.StringWidth(line)-2)
	}
	if pad := inner - ansi.StringWidth(line); pad > 0 {
		line += plain.Render(strings.Repeat(" ", pad))
	}
	return line
}

// pickerPanel is the review picker: the submodules checked out in the ticket's worktree.
func (m *model) pickerPanel(r row, w, h int) string {
	inner := w - 4
	nameW, branchW, sumW := 0, 0, 0
	for _, ri := range m.repos {
		nameW, branchW, sumW = max(nameW, len(ri.Name)), max(branchW, len(ri.Branch)), max(sumW, len(ri.summary()))
	}
	var body []string
	start := max(0, m.repoCur-(h-3))
	for i := start; i < len(m.repos); i++ {
		ri, sel := m.repos[i], i == m.repoCur
		bg := func(s lipgloss.Style) lipgloss.Style {
			if sel {
				return s.Background(cSelBg)
			}
			return s
		}
		plain := bg(lipgloss.NewStyle())
		bar := " "
		if sel {
			bar = bg(stTitle).Render("▌")
		}
		sumSt := stLabel
		if ri.summary() == "no changes" {
			sumSt = stFaint
		}
		line := bar + plain.Render(" ") + bg(lipgloss.NewStyle().Bold(true)).Render(fmt.Sprintf("%-*s", nameW, ri.Name)) +
			plain.Render("  ") + bg(stDim).Render(fmt.Sprintf("%-*s", branchW, ri.Branch)) +
			plain.Render("  ") + bg(sumSt).Render(fmt.Sprintf("%-*s", sumW, ri.summary()))
		if pad := inner - ansi.StringWidth(line); pad > 0 {
			line += plain.Render(strings.Repeat(" ", pad))
		}
		body = append(body, line)
	}
	title := stTitle.Render("review")
	if r.Type != "" {
		title += "  " + typeStyle(r).Bold(true).Render(typeLabel(r))
	}
	title += " " + lipgloss.NewStyle().Bold(true).Render(m.reviewTicket)
	right := ""
	if r.hasStage() {
		right = m.pipeline(r, func(s lipgloss.Style) lipgloss.Style { return s }, max(10, w/2))
	}
	return panel(title, right, body, w, h)
}

func typeColour(r row) lipgloss.Color {
	if r.Work != nil && r.Work.Colour != "" {
		return lipgloss.Color(r.Work.Colour)
	}
	return cAccent
}

// typeLabel is how the row's type is shown: its label if it has one (XPLR), else its name in capitals.
func typeLabel(r row) string {
	if r.Work != nil && r.Work.Label != "" {
		return r.Work.Label
	}
	return strings.ToUpper(r.Type)
}

func typeStyle(r row) lipgloss.Style { return lipgloss.NewStyle().Foreground(typeColour(r)) }

// pipeline renders the row's stages as "done › [ current ] › upcoming". Approval stages fold
// into the stage they approve: "[ implement ◆ ]" while waiting for your approval of it.
// If it doesn't fit, finished stages drop off the front, then upcoming ones off the end.
func (m *model) pipeline(r row, bg func(lipgloss.Style) lipgloss.Style, max int) string {
	st, cur := r.Work.Stages, r.St.Stage
	skipped := map[int]bool{}
	for _, i := range r.St.Skipped {
		skipped[i] = true
	}
	col := typeStyle(r)
	var parts []string
	curPart := 0
	for i, s := range st {
		if s == approveStage {
			continue
		}
		gate := i+1 < len(st) && st[i+1] == approveStage && i+1 == cur
		switch {
		case i == cur || gate:
			t := bg(col.Bold(true)).Render("[ " + s)
			if gate {
				t += bg(lipgloss.NewStyle()).Render(" ") + bg(m.diamond(r)).Render("◆")
			}
			parts = append(parts, t+bg(col.Bold(true)).Render(" ]"))
			curPart = len(parts) - 1
		case skipped[i]:
			parts = append(parts, bg(stFaint.Strikethrough(true)).Render(s))
		case i < cur:
			parts = append(parts, bg(col.Faint(true)).Render(s))
		default:
			parts = append(parts, bg(stFuture).Render(s))
		}
	}
	sep, ell := bg(stFaint).Render(" › "), bg(stFaint).Render("…")
	lo, hi := 0, len(parts)-1
	for {
		var shown []string
		if lo > 0 {
			shown = append(shown, ell)
		}
		shown = append(shown, parts[lo:hi+1]...)
		if hi < len(parts)-1 {
			shown = append(shown, ell)
		}
		out := strings.Join(shown, sep)
		switch {
		case ansi.StringWidth(out) <= max:
			return out
		case lo < curPart-1:
			lo++
		case hi > curPart+1:
			hi--
		default:
			return ansi.Truncate(out, max, "…")
		}
	}
}

// diamond is the approval marker. Its colour eases from the type colour towards the
// background and back, so it pulses gently while the work waits for you.
func (m *model) diamond(r row) lipgloss.Style {
	const period, depth = 1.6, 0.7 // seconds per pulse; how far it fades (1 = all the way)
	k := 0.5 - 0.5*math.Cos(2*math.Pi*float64(m.now.UnixMilli())/1000/period)
	c, err := colorful.Hex(string(typeColour(r)))
	if err != nil {
		return typeStyle(r).Bold(true)
	}
	bg, _ := colorful.Hex(pulseBg)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c.BlendLab(bg, k*depth).Clamped().Hex())).Bold(true)
}

func (m *model) details(r row) []string {
	label := func(s string) string { return stLabel.Render(fmt.Sprintf("%-9s", s)) }
	blurb := lipgloss.NewStyle().Bold(true).Render(r.St.Blurb)
	if r.St.Blurb == "" {
		blurb = stDim.Italic(true).Render("no blurb yet")
	}
	repos, where := stDim.Render("—"), stDim.Render("—")
	switch {
	case r.WT != nil:
		if len(r.WT.Repos) > 0 {
			repos = strings.Join(r.WT.Repos, stFaint.Render(", "))
		} else {
			repos = stDim.Render("none checked out")
		}
		where = stDim.Render(tildify(r.WT.Path))
		// worktree dirs are named after their branch; only show the branch if it differs
		if r.WT.Branch != filepath.Base(r.WT.Path) {
			where += stFaint.Render("  ·  ") + r.WT.Branch
		}
	case r.alive():
		where = stDim.Italic(true).Render("setting up…")
	}
	return []string{
		blurb,
		label("repos") + repos,
		label("worktree") + where,
	}
}

func (m *model) counts() string {
	c := map[string]int{}
	for _, r := range m.rows {
		c[r.Status]++
	}
	var parts []string
	if n := c[dWaiting]; n > 0 {
		parts = append(parts, stWaiting.Render(fmt.Sprintf("! %d", n)))
	}
	if n := c[dDone]; n > 0 {
		parts = append(parts, stDone.Render(fmt.Sprintf("● %d", n)))
	}
	if n := c[dWorking] + c[dStarting]; n > 0 {
		parts = append(parts, stWorking.Render(fmt.Sprintf("%s %d", spinner[m.frame%len(spinner)], n)))
	}
	return strings.Join(parts, "  ")
}

func (m *model) icon(status string) (string, lipgloss.Style) {
	switch status {
	case dWaiting:
		return "!", stWaiting
	case dDone:
		return "●", stDone
	case dWorking:
		return spinner[m.frame%len(spinner)], stWorking
	case dStarting:
		return spinner[m.frame%len(spinner)], stDim
	case dStale:
		return "◌", stDim
	case dIdle:
		return "○", stLabel
	case dExited:
		return "✗", stDim
	default:
		return "·", stFaint
	}
}

func (m *model) footer() string {
	switch m.mode {
	case mNew, mReply:
		return " " + m.input.View()
	case mConfirm:
		return " " + stDone.Render(m.confirmQ)
	}
	if m.flash != "" && time.Since(m.flashAt) < 6*time.Second {
		if m.flashOK {
			return " " + stOK.Render(m.flash)
		}
		return " " + stErr.Render(m.flash)
	}
	keys := [][2]string{{"j/k", "move"}, {"enter", "open"}, {"n", "new"}, {"i", "reply"}, {"d", "review"}, {"f", "finish"}, {"x", "kill"}, {"?", "help"}, {"q", "quit"}}
	if m.mode == mReview {
		keys = [][2]string{{"enter", "review in tuicr"}}
		if r, ok := m.reviewRow(); ok && r.atGate() {
			keys = append(keys, [2]string{"a", "approve"})
		}
		keys = append(keys, [2]string{"l", "open session"}, [2]string{"esc", "back"})
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, stKey.Render(k[0])+" "+stDim.Render(k[1]))
	}
	return ansi.Truncate(" "+strings.Join(parts, stFaint.Render("  ·  ")), m.w, "…")
}

func (m *model) helpView() string {
	keys := [][2]string{
		{"j / k", "move"},
		{"g / G", "top / bottom"},
		{"enter, l", "open the session (resumes the conversation if Claude isn't running)"},
		{"n", "new session: a ticket (5170 or PROJ-5170) or an exploration name, then optionally what it's about"},
		{"i", "send a reply to the selected session without switching to it"},
		{"d", "review: pick a submodule and comment on its diff in tuicr; exporting (y) sends the comments"},
		{"f", "send the finish prompt (check it's pushed, then finish-feature)"},
		{"x", "kill the Claude window (keeps the worktree)"},
		{"r", "refresh now"},
		{"q", "quit (esc also quits in --popup mode)"},
	}
	icons := [][3]string{
		{"!", "waiting", "asking for permission or asked you a question"},
		{"●", "done", "finished a turn you haven't looked at yet"},
		{"⠋", "working", "Claude is running"},
		{"○", "idle", "waiting for your next prompt"},
		{"◌", "idle?", "hooks say working, but nothing is running on screen (Esc?)"},
		{"·", "no claude", "worktree exists, Claude isn't running"},
		{"◆", "approval", "[ implement ◆ ]: the work is waiting for you to approve that stage"},
	}
	var lines []string
	for _, k := range keys {
		lines = append(lines, stKey.Render(fmt.Sprintf("%-10s", k[0]))+k[1])
	}
	lines = append(lines, "")
	for _, ic := range icons {
		st := stLabel
		switch ic[0] {
		case "!":
			st = stWaiting
		case "●":
			st = stDone
		case "⠋":
			st = stWorking
		case "◌", "·":
			st = stDim
		case "◆":
			st = stTitle
		}
		lines = append(lines, st.Render(ic[0])+"  "+st.Render(fmt.Sprintf("%-10s", ic[1]))+stDim.Render(ic[2]))
	}
	lines = append(lines, "", stDim.Render("Pipelines: dim stages are done, [ bracketed ] is current, struck-through were skipped."),
		"", stDim.Render("press any key"))
	w := min(m.w, 90)
	return panel(stTitle.Render("help"), "", lines, w, min(m.h, len(lines)+2))
}
