package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/osteele/gitsync/internal/actions"
	"github.com/osteele/gitsync/internal/report"
	"github.com/osteele/gitsync/internal/scan"
	"github.com/osteele/gitsync/internal/vcs"
)

// chromeHeight is the number of lines View always spends around the repo
// list: the title, the blank line under it, the blank line above the help,
// and the help line itself. Message blocks and scroll indicators are
// reserved on top of this, in listHeight.
const chromeHeight = 4

type tuiItem struct {
	path   string
	status *vcs.RepoStatus
	busy   bool
	// depth is the indentation level: 0 for a top-level row, one more per
	// expansion level below it.
	depth int
	// expanded reports whether a container's children are showing. kids
	// caches the descendant rows — including any deeper expanded subtrees —
	// across a collapse, so re-expanding does not rescan.
	expanded bool
	kids     []tuiItem
	// lastResult holds the full text of the most recent action result,
	// which is often multi-line command output worth reading in full.
	lastResult string
	// roll aggregates the repositories beneath a container. It arrives
	// after the row is already on screen, since computing it means scanning
	// the whole subtree.
	roll     rollup
	rolledUp bool
}

func (i tuiItem) name() string {
	return filepath.Base(i.path)
}

// expandable reports whether the row is a directory that can be expanded
// to reveal its contents. The answer is unknown until the status loads.
func (i tuiItem) expandable() bool {
	return i.status != nil && i.status.Type == vcs.Dir
}

// Status glyphs are text-presentation and exactly one cell wide. Emoji
// render at inconsistent widths (a pencil is two cells, an arrow one), and
// concatenating them is what made the arrow column wander and the layout
// shift as statuses arrived.
const (
	glyphDirty     = "●"
	glyphAhead     = "↑"
	glyphBehind    = "↓"
	glyphClean     = "✓"
	glyphNoRemote  = "⌂"
	glyphCorrupted = "✗"
	glyphError     = "!"
	glyphBusy      = "⋯"
	glyphLoading   = "·"
	glyphBlank     = " "
)

// badgeSlots is the number of fixed one-cell status slots per row.
const badgeSlots = 3

// icon renders status as three fixed slots: dirty, sync, health. Each slot
// holds one meaning and one cell, so a glyph always appears in the same
// column whether or not its neighbours are present. It reports status
// only, never type -- the type has its own column.
func (i tuiItem) icon() string {
	slot := [badgeSlots]string{glyphBlank, glyphBlank, glyphBlank}
	switch {
	case i.busy:
		slot[0] = glyphBusy
	case i.status == nil:
		slot[0] = glyphLoading
	case i.status.Corrupted:
		slot[2] = glyphCorrupted
	case i.status.Error != "":
		slot[2] = glyphError
	case i.status.Type == vcs.Dir:
		// A directory's state is its rollup, which the status column
		// carries; the disclosure marker already identifies it.
	default:
		if i.status.Dirty {
			slot[0] = glyphDirty
		}
		switch {
		case i.status.Ahead.Positive():
			slot[1] = glyphAhead
		case i.status.Behind.Positive():
			slot[1] = glyphBehind
		}
		switch {
		case !i.status.Remote:
			// Local only: the house says where the sole copy lives. It must
			// not borrow the everything-is-fine checkmark, since nothing is
			// backing this repository up.
			slot[2] = glyphNoRemote
		case slot[0] == glyphBlank && slot[1] == glyphBlank:
			slot[2] = glyphClean
		}
	}
	return strings.Join(slot[:], "")
}

// typeIcon is the VCS column, one fixed-width slot to the left of the
// name: a folder for a container, the branch key for Git, and the letter
// j for Jujutsu.
//
// The two repository glyphs are deliberately from different families. A
// second fork-shaped mark beside Git's branch reads as "some VCS squiggle"
// at a glance and has to be looked at twice; a letterform next to line art
// is told apart without focusing.
func (i tuiItem) typeIcon() string {
	if i.status == nil {
		return glyphBlank
	}
	switch i.status.Type {
	case vcs.Jujutsu:
		return "ⅉ"
	case vcs.Git:
		return "⎇"
	default:
		return "📁"
	}
}

// typeIconWidth is the fixed width of the type column. The folder is an
// emoji and renders two cells wide; the branch and fork glyphs render one,
// so the column is padded to the wider of them.
const typeIconWidth = 2

// total aggregates the whole scan: repositories at the top level counted
// directly, and everything beneath a container taken from its rollup. Only
// top-level rows are consulted, so an expanded container's children are
// not counted twice.
func (m model) total() (rollup, bool) {
	var r rollup
	complete := true
	for _, item := range m.items {
		if item.depth > 0 {
			continue
		}

		if item.status == nil {
			complete = false
			continue
		}
		if item.status.Type == vcs.Dir {
			if !item.rolledUp {
				complete = false
				continue
			}
			r = r.plus(item.roll)
			continue
		}
		r = r.add(item.status)
	}
	return r, complete
}

// disclosure is the expand/collapse marker, in a fixed slot so names line
// up whether or not a row can expand.
func (i tuiItem) disclosure() string {
	switch {
	case i.expanded:
		return "▾ "
	case i.expandable():
		return "▸ "
	}
	return "  "
}

// rowPrefix is everything left of the detail column: the nesting indent,
// the status slots, the type icon, the disclosure marker, and the name.
//
// The indent leads the whole group rather than only the name, so a child's
// icons sit beneath its parent's and the nesting is legible from the
// glyphs alone. The group is padded to one width across all rows, which is
// what keeps the detail column itself from stepping right with the indent.
func (m model) rowPrefix(i tuiItem) string {
	return strings.Repeat("  ", i.depth) +
		i.icon() + " " +
		pad(i.typeIcon(), typeIconWidth) + " " +
		i.disclosure() + i.name()
}

// prefixWidth is the width of everything left of the detail column,
// measured across *every* row rather than only the visible ones. Its
// inputs -- indent depth, name, and the fixed-width slots -- are all known
// as soon as the directory is listed, so it cannot shift when statuses
// arrive later or when the list scrolls, which is what made the columns
// jump.
func (m model) prefixWidth() int {
	w := 0
	for _, item := range m.items {
		w = max(w, lipgloss.Width(m.rowPrefix(item)))
	}
	return w
}

// pad right-pads s to w terminal cells.
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// statusText is the status column. It excludes the repo type, which has
// its own column.
func (i tuiItem) statusText() string {
	if i.status == nil {
		return "loading…"
	}
	// A container's status is the state of what it holds. Until the
	// subtree scan lands, fall back to the shallow count so the row is
	// never blank.
	if i.status.Type == vcs.Dir && i.rolledUp {
		return i.roll.text()
	}
	tokens := report.StatusTokens(i.status)
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		parts = append(parts, tok.Text)
	}
	text := strings.Join(parts, " ")
	if i.busy {
		if text == "" {
			text = "…"
		} else {
			text += "…"
		}
	}
	return text
}

type statusMsg struct {
	path   string
	status *vcs.RepoStatus
}

// childrenMsg carries the result of lazily scanning a container's children
// on first expansion.
type childrenMsg struct {
	path     string
	statuses []*vcs.RepoStatus
	err      error
}

type actionDoneMsg struct {
	message string
	path    string
	failed  bool
}

// bulkResultMsg carries one finished repository from a running bulk batch;
// ch yields the rest. bulkDoneMsg ends the batch with the final tallies.
type bulkResultMsg struct {
	op        actions.BulkOp
	result    actions.Result
	ch        <-chan actions.Result
	succeeded int
	failed    int
}

type bulkDoneMsg struct {
	op        actions.BulkOp
	succeeded int
	failed    int
}

// draftMsg carries a proposed commit message from the AI commit tool.
type draftMsg struct {
	path    string
	message string
	err     error
}

type clearMsg struct{}

// pendingAction is a destructive action awaiting y/n confirmation.
type pendingAction struct {
	prompt string
	run    func(m model) (model, tea.Cmd)
}

type model struct {
	scanDir string
	items   []tuiItem
	cursor  int
	offset  int
	message string
	// messageSticky keeps a failure message on screen until the next
	// keypress; command output is worth reading and must not time out.
	messageSticky bool
	width         int
	height        int
	help          help.Model
	keys          keyMap
	pending       *pendingAction
	commitInput   textinput.Model
	// drafting reports that a commit message is being generated for
	// draftPath; the prompt is already open and editable meanwhile.
	drafting   bool
	draftPath  string
	detail     viewport.Model
	showDetail bool
}

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Expand    key.Binding
	Collapse  key.Binding
	Push      key.Binding
	Pull      key.Binding
	Commit    key.Binding
	Sync      key.Binding
	PushAll   key.Binding
	PullAll   key.Binding
	CommitAll key.Binding
	SyncAll   key.Binding
	Repair    key.Binding
	AddRemote key.Binding
	Open      key.Binding
	Reveal    key.Binding
	Detail    key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Commit, k.Sync, k.Detail, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Expand, k.Collapse, k.Push, k.Pull, k.Commit, k.Sync},
		{k.PushAll, k.PullAll, k.CommitAll, k.SyncAll, k.Repair, k.AddRemote, k.Open, k.Reveal, k.Detail, k.Quit},
	}
}

var defaultKeyMap = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Expand: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "expand"),
	),
	Collapse: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "collapse"),
	),
	Push: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "push"),
	),
	Pull: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "pull/fetch"),
	),
	Commit: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "commit"),
	),
	Sync: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sync"),
	),
	PushAll: key.NewBinding(
		key.WithKeys("P"),
		key.WithHelp("P", "push all"),
	),
	PullAll: key.NewBinding(
		key.WithKeys("U"),
		key.WithHelp("U", "pull all"),
	),
	CommitAll: key.NewBinding(
		key.WithKeys("C"),
		key.WithHelp("C", "commit all"),
	),
	SyncAll: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "sync all"),
	),
	Repair: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "repair"),
	),
	AddRemote: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "+gh remote"),
	),
	Open: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open editor"),
	),
	Reveal: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "reveal"),
	),
	Detail: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "details"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "more keys"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

// RunTUI launches the interactive TUI over the subdirectories of scanDir.
func RunTUI(scanDir string) error {
	if _, err := os.Stat(scanDir); err != nil {
		return fmt.Errorf("invalid scan directory: %w", err)
	}
	m, err := newModel(scanDir)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newModel(scanDir string) (model, error) {
	subdirs, err := scan.GetSubdirectories(scanDir)
	if err != nil {
		return model{}, fmt.Errorf("cannot scan %s: %w", scanDir, err)
	}
	items := make([]tuiItem, 0, len(subdirs))
	for _, d := range subdirs {
		items = append(items, tuiItem{path: d})
	}
	input := textinput.New()
	input.Prompt = "commit message: "
	input.CharLimit = 200
	return model{
		scanDir:     scanDir,
		items:       items,
		help:        help.New(),
		keys:        defaultKeyMap,
		commitInput: input,
	}, nil
}

func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.items))
	for i := range m.items {
		cmds = append(cmds, loadStatusCmd(m.items[i].path))
	}
	return tea.Batch(cmds...)
}

// findByPath locates a row by path. Rows come and go as containers expand
// and collapse, so messages in flight must address rows by path, not index.
func (m model) findByPath(path string) int {
	for i, item := range m.items {
		if item.path == path {
			return i
		}
	}
	return -1
}

func loadStatusCmd(path string) tea.Cmd {
	return func() tea.Msg {
		status, err := vcs.GetRepoStatus(path)
		if err != nil {
			// A scan error is not the same as corruption: keep whatever the
			// status detection produced (including its Corrupted verdict) and
			// surface the error without forcing the repair-dangerous state.
			if status == nil {
				status = &vcs.RepoStatus{Path: path, Type: vcs.Dir}
			}
			if status.Error == "" {
				status.Error = err.Error()
			}
		}
		if status.Type == vcs.Dir {
			status.NestedRepos = scan.CountNestedRepos(path)
		}
		return statusMsg{path: path, status: status}
	}
}

// rollupDepth caps how deep a container's rollup looks for repositories.
// Repositories in this tree sit up to three levels below a container, and
// the descent stops at each repository anyway.
const rollupDepth = 4

// rollup is the aggregate state of the repositories beneath a directory.
type rollup struct {
	repos  int
	dirty  int
	ahead  int
	behind int
}

func (r rollup) add(s *vcs.RepoStatus) rollup {
	if s.Type == vcs.Dir {
		return r
	}
	r.repos++
	if s.Dirty {
		r.dirty++
	}
	if s.Ahead.Positive() {
		r.ahead++
	}
	if s.Behind.Positive() {
		r.behind++
	}
	return r
}

func (r rollup) plus(o rollup) rollup {
	return rollup{r.repos + o.repos, r.dirty + o.dirty, r.ahead + o.ahead, r.behind + o.behind}
}

// text renders the rollup for a status column: the repository count plus
// only the states that are actually present, so a quiet subtree stays
// quiet.
func (r rollup) text() string {
	if r.repos == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("%d %s", r.repos, plural(r.repos, "repo"))}
	if r.dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", r.dirty))
	}
	if r.ahead > 0 {
		parts = append(parts, fmt.Sprintf("%d ahead", r.ahead))
	}
	if r.behind > 0 {
		parts = append(parts, fmt.Sprintf("%d behind", r.behind))
	}
	return strings.Join(parts, ", ")
}

// itemText renders the totals line: how many rows are listed, how many
// repositories they account for, and the states worth acting on.
func (r rollup) itemText(items int) string {
	parts := []string{fmt.Sprintf("%d %s", items, plural(items, "item"))}
	if t := r.text(); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// rollupMsg carries a container's aggregated descendant state.
type rollupMsg struct {
	path string
	r    rollup
}

// loadRollupCmd aggregates the state of every repository beneath a
// container. This is the expensive part of the view -- it scans the whole
// subtree -- so it runs as a background command per container and the row
// shows its plain count until the result lands.
func loadRollupCmd(path string) tea.Cmd {
	return func() tea.Msg {
		statuses, err := scan.Scan(path, scan.Options{Recursive: true, MaxDepth: rollupDepth})
		if err != nil {
			return rollupMsg{path: path}
		}
		var r rollup
		for _, s := range statuses {
			r = r.add(s)
		}
		return rollupMsg{path: path, r: r}
	}
}

// loadChildrenCmd scans a container's immediate children through the
// bounded-worker scanner, so expansion cost is one readdir plus one
// bounded status pass.
func loadChildrenCmd(path string) tea.Cmd {
	return func() tea.Msg {
		subdirs, err := scan.GetSubdirectories(path)
		if err != nil {
			return childrenMsg{path: path, err: err}
		}
		statuses, _ := scan.ScanDirectories(subdirs)
		sort.Slice(statuses, func(i, j int) bool {
			return filepath.Base(statuses[i].Path) < filepath.Base(statuses[j].Path)
		})
		return childrenMsg{path: path, statuses: statuses}
	}
}

// listHeight is the number of repo rows that fit on screen, after
// reserving the lines View spends on chrome: the title, the separating
// blanks, the help line, whichever message block is showing, and the
// scroll indicators when the list does not fit.
func (m model) listHeight() int {
	reserved := chromeHeight
	if len(m.items) > 0 {
		reserved += 2 // the rule and the totals line closing the table
	}
	switch {
	case m.commitInput.Focused():
		reserved += 3
	case m.pending != nil:
		reserved += 2
	case m.message != "":
		reserved += 2
		if strings.Contains(m.message, "\n") {
			reserved++ // the "enter for full output" hint
		}
	}
	// Reserving indicator space unconditionally for an oversized list keeps
	// this independent of the offset, which is itself derived from here.
	if len(m.items) > m.height-reserved {
		reserved += 2
	}
	h := m.height - reserved
	if h < 1 {
		return 1
	}
	return h
}

// clampView scrolls the window so the cursor stays visible.
func (m model) clampView() model {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset > len(m.items)-h {
		m.offset = len(m.items) - h
	}
	if m.offset < 0 {
		m.offset = 0
	}
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.detail.Width = msg.Width
		m.detail.Height = msg.Height - 2
		m = m.clampView()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case statusMsg:
		idx := m.findByPath(msg.path)
		if idx < 0 {
			return m, nil
		}
		m.items[idx].status = msg.status
		// A container's rollup can only start once its type is known.
		if msg.status.Type == vcs.Dir && !m.items[idx].rolledUp {
			return m, loadRollupCmd(msg.path)
		}
		return m, nil

	case rollupMsg:
		if idx := m.findByPath(msg.path); idx >= 0 {
			m.items[idx].roll = msg.r
			m.items[idx].rolledUp = true
		}
		return m, nil

	case childrenMsg:
		idx := m.findByPath(msg.path)
		if idx < 0 {
			return m, nil
		}
		m.items[idx].busy = false
		if msg.err != nil {
			m.message = fmt.Sprintf("expand %s failed: %v", m.items[idx].name(), msg.err)
			m.messageSticky = true
			return m, nil
		}
		kids := make([]tuiItem, 0, len(msg.statuses))
		for _, status := range msg.statuses {
			kids = append(kids, tuiItem{path: status.Path, depth: m.items[idx].depth + 1, status: status})
		}
		m.items[idx].kids = kids
		m.items[idx].expanded = true
		m.items = insertItems(m.items, idx+1, kids)
		return m.clampView(), nil

	case actionDoneMsg:
		m.message = msg.message
		idx := m.findByPath(msg.path)
		var cmds []tea.Cmd
		if idx >= 0 {
			m.items[idx].busy = false
			m.items[idx].lastResult = msg.message
			cmds = append(cmds, refreshStatusCmd(m.items[idx].path))
		}
		// Failures stay until the next keypress: their command output is
		// exactly what the user needs to read.
		m.messageSticky = msg.failed
		if !msg.failed {
			cmds = append(cmds, clearAfter(3*time.Second))
		}
		return m, tea.Batch(cmds...)

	case bulkResultMsg:
		var cmds []tea.Cmd
		if idx := m.findByPath(msg.result.Item.Status.Path); idx >= 0 {
			m.items[idx].busy = false
			name := m.items[idx].name()
			if msg.result.Err != nil {
				m.items[idx].lastResult = fmt.Sprintf("%s %s failed: %v", msg.op, name, msg.result.Err)
			} else {
				m.items[idx].lastResult = fmt.Sprintf("%s %s done", msg.op, name)
			}
			cmds = append(cmds, refreshStatusCmd(m.items[idx].path))
		}
		succeeded, failed := msg.succeeded, msg.failed
		if msg.result.Err != nil {
			failed++
		} else {
			succeeded++
		}
		cmds = append(cmds, awaitBulkResult(msg.op, msg.ch, succeeded, failed))
		return m, tea.Batch(cmds...)

	case bulkDoneMsg:
		summary := fmt.Sprintf("%s: %d %s, %d failed", msg.op, msg.succeeded, msg.op.PastTense(), msg.failed)
		m.message = summary
		m.messageSticky = msg.failed > 0
		if msg.failed == 0 {
			return m, clearAfter(3 * time.Second)
		}
		return m, nil

	case draftMsg:
		if msg.path != m.draftPath {
			return m, nil // the prompt moved on to another repository
		}
		m.drafting = false
		if msg.err != nil || strings.TrimSpace(msg.message) == "" {
			return m, nil // keep the canned default; the prompt still works
		}
		// Only seed an untouched prompt: replacing text the user has begun
		// editing would be worse than offering no draft at all.
		if m.commitInput.Focused() && m.commitInput.Value() == actions.DefaultCommitMessage {
			m.commitInput.SetValue(msg.message)
			m.commitInput.CursorEnd()
		}
		return m, nil

	case clearMsg:
		if !m.messageSticky {
			m.message = ""
		}
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A sticky failure message is dismissed by the next keypress.
	if m.messageSticky {
		m.messageSticky = false
		m.message = ""
	}

	if m.commitInput.Focused() {
		return m.handleCommitInputKey(msg)
	}
	if m.pending != nil {
		return m.handleConfirmKey(msg)
	}
	if m.showDetail {
		switch msg.String() {
		case "esc", "q", "enter":
			m.showDetail = false
			return m, nil
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}

	// Esc dismisses the expanded help, matching the detail view and the
	// prompts. It is only a dismissal, so it does nothing when the help is
	// already collapsed.
	if msg.String() == "esc" && m.help.ShowAll {
		m.help.ShowAll = false
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m.clampView(), nil

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		return m.clampView(), nil

	case key.Matches(msg, m.keys.Expand):
		return m.expand()

	case key.Matches(msg, m.keys.Collapse):
		return m.collapse()

	case key.Matches(msg, m.keys.Detail):
		return m.showDetailView()

	case key.Matches(msg, m.keys.Push):
		return m.runAction("push", actions.ActionPush)

	case key.Matches(msg, m.keys.Pull):
		return m.runAction("pull", actions.ActionPull)

	case key.Matches(msg, m.keys.Commit):
		return m.startCommit()

	case key.Matches(msg, m.keys.Sync):
		return m.runAction("sync", actions.ActionSync)

	case key.Matches(msg, m.keys.PushAll):
		return m.startBulk(actions.BulkPush)

	case key.Matches(msg, m.keys.PullAll):
		return m.startBulk(actions.BulkPull)

	case key.Matches(msg, m.keys.CommitAll):
		return m.startBulk(actions.BulkCommit)

	case key.Matches(msg, m.keys.SyncAll):
		return m.startBulk(actions.BulkSync)

	case key.Matches(msg, m.keys.Repair):
		return m.confirmRepair()

	case key.Matches(msg, m.keys.AddRemote):
		return m.confirmAddRemote()

	case key.Matches(msg, m.keys.Open):
		return m.openEditor()

	case key.Matches(msg, m.keys.Reveal):
		return m.revealInFinder()
	}
	return m, nil
}

// current returns the selected item and whether the selection is valid and
// free to act on.
func (m model) current() (tuiItem, bool) {
	if len(m.items) == 0 {
		return tuiItem{}, false
	}
	return m.items[m.cursor], true
}

// insertItems splices rows into items at index at.
func insertItems(items []tuiItem, at int, rows []tuiItem) []tuiItem {
	out := make([]tuiItem, 0, len(items)+len(rows))
	out = append(out, items[:at]...)
	out = append(out, rows...)
	out = append(out, items[at:]...)
	return out
}

// expand reveals the selected container's children. The first expansion
// scans them through the bounded-worker scanner, showing the row's busy
// state while that runs; later expansions reuse the cached rows.
func (m model) expand() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok || !item.expandable() || item.expanded || item.busy {
		return m, nil
	}
	if item.kids != nil {
		m.items[m.cursor].expanded = true
		m.items = insertItems(m.items, m.cursor+1, item.kids)
		return m.clampView(), nil
	}
	m.items[m.cursor].busy = true
	return m, loadChildrenCmd(item.path)
}

// collapse hides the selected container's descendants, keeping them cached
// for the next expansion.
func (m model) collapse() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok || !item.expanded {
		return m, nil
	}
	end := m.cursor + 1
	for end < len(m.items) && m.items[end].depth > item.depth {
		end++
	}
	m.items[m.cursor].kids = append([]tuiItem(nil), m.items[m.cursor+1:end]...)
	m.items[m.cursor].expanded = false
	m.items = append(m.items[:m.cursor+1], m.items[end:]...)
	return m.clampView(), nil
}

func (m model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pending := m.pending
	switch msg.String() {
	case "y", "Y":
		m.pending = nil
		return pending.run(m)
	case "n", "N", "esc", "ctrl+c":
		m.pending = nil
		m.message = "cancelled"
		return m, clearAfter(2 * time.Second)
	}
	// Any other key is ignored while a confirmation is pending.
	return m, nil
}

func (m model) handleCommitInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		message := strings.TrimSpace(m.commitInput.Value())
		if message == "" {
			message = actions.DefaultCommitMessage
		}
		m.commitInput.Blur()
		m.commitInput.SetValue("")
		m.drafting, m.draftPath = false, ""
		return m.runAction("commit", func(path string) error {
			return actions.ActionCommit(path, message)
		})
	case tea.KeyEsc, tea.KeyCtrlC:
		m.commitInput.Blur()
		m.commitInput.SetValue("")
		m.drafting, m.draftPath = false, ""
		m.message = "commit cancelled"
		return m, clearAfter(2 * time.Second)
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m model) startCommit() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	m.commitInput.SetValue(actions.DefaultCommitMessage)
	m.commitInput.CursorEnd()
	cmds := []tea.Cmd{m.commitInput.Focus()}
	// Drafting reaches an LLM and takes seconds, so the prompt opens
	// immediately on the canned message and the draft replaces it when it
	// lands. Typing is never blocked, and never interrupted -- the draft is
	// dropped if the user has already started editing.
	if actions.CommitToolAvailable(item.path) {
		m.drafting = true
		m.draftPath = item.path
		cmds = append(cmds, draftCommitCmd(item.path))
	}
	return m, tea.Batch(cmds...)
}

// draftCommitCmd asks the AI commit tool for a proposed message.
func draftCommitCmd(path string) tea.Cmd {
	return func() tea.Msg {
		message, err := actions.DraftCommitMessage(path)
		return draftMsg{path: path, message: message, err: err}
	}
}

func (m model) busyMessage(item tuiItem) (tea.Model, tea.Cmd) {
	m.message = fmt.Sprintf("%s: action in progress", item.name())
	return m, clearAfter(2 * time.Second)
}

func (m model) showDetailView() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	var b strings.Builder
	b.WriteString(item.path + "\n\n")
	b.WriteString("status: " + item.statusText() + "\n")
	if item.status != nil && item.status.Error != "" {
		b.WriteString("\nerror:\n" + item.status.Error + "\n")
	}
	if item.lastResult != "" {
		b.WriteString("\nlast action:\n" + item.lastResult + "\n")
	} else {
		b.WriteString("\nno actions run yet\n")
	}
	height := m.height - 2
	if height < 1 {
		height = 1
	}
	m.detail = viewport.New(m.width, height)
	m.detail.SetContent(b.String())
	m.showDetail = true
	return m, nil
}

func (m model) confirmRepair() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	m.pending = &pendingAction{
		prompt: fmt.Sprintf("repair %s? this moves .git aside (y/n)", item.name()),
		run:    func(m model) (model, tea.Cmd) { return m.runRepair() },
	}
	return m, nil
}

func (m model) confirmAddRemote() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	// Adding a first remote is safe; replacing an existing origin is not.
	if !vcs.HasOrigin(item.path) {
		mm, cmd := m.runAction("add GitHub remote", actions.ActionAddGitHubRemote)
		return mm, cmd
	}
	m.pending = &pendingAction{
		prompt: fmt.Sprintf("replace origin of %s with the GitHub remote? (y/n)", item.name()),
		run: func(m model) (model, tea.Cmd) {
			mm, cmd := m.runAction("add GitHub remote", actions.ActionAddGitHubRemote)
			return mm.(model), cmd
		},
	}
	return m, nil
}

func (m model) runAction(name string, fn func(string) error) (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	m.items[m.cursor].busy = true
	m.message = fmt.Sprintf("%s: %s…", name, item.name())
	return m, func() tea.Msg {
		if err := fn(item.path); err != nil {
			return actionDoneMsg{
				path:    item.path,
				message: fmt.Sprintf("%s %s failed: %v", name, item.name(), err),
				failed:  true,
			}
		}
		return actionDoneMsg{path: item.path, message: fmt.Sprintf("%s %s done", name, item.name())}
	}
}

// startBulk plans a bulk operation over every eligible repository currently
// in scope and asks for confirmation, showing the count and the first few
// names. An empty plan just reports that there is nothing to do.
func (m model) startBulk(op actions.BulkOp) (tea.Model, tea.Cmd) {
	var statuses []*vcs.RepoStatus
	for _, item := range m.items {
		if item.status != nil {
			statuses = append(statuses, item.status)
		}
	}
	items := actions.Plan(op, statuses)
	if len(items) == 0 {
		m.message = actions.EmptyMessage(op)
		return m, clearAfter(2 * time.Second)
	}
	if op == actions.BulkCommit {
		if err := actions.PreflightCommitTools(items); err != nil {
			m.message = err.Error()
			m.messageSticky = true
			return m, nil
		}
	}
	const showNames = 3
	names := make([]string, 0, showNames+1)
	for i, item := range items {
		if i == showNames {
			names = append(names, "…")
			break
		}
		names = append(names, filepath.Base(item.Status.Path))
	}
	noun := "repositories"
	if len(items) == 1 {
		noun = "repository"
	}
	m.pending = &pendingAction{
		prompt: fmt.Sprintf("%s %d %s (%s)? (y/n)", op, len(items), noun, strings.Join(names, ", ")),
		run:    func(m model) (model, tea.Cmd) { return m.runBulk(op, items) },
	}
	return m, nil
}

// runBulk marks every planned repository busy and starts the batch. The
// engine runs in a goroutine feeding a channel; awaitBulkResult turns each
// result into a message so rows refresh as they finish.
func (m model) runBulk(op actions.BulkOp, items []actions.PlanItem) (model, tea.Cmd) {
	for _, item := range items {
		if idx := m.findByPath(item.Status.Path); idx >= 0 {
			m.items[idx].busy = true
		}
	}
	ch := make(chan actions.Result, len(items))
	go func() {
		actions.ExecuteBulk(op, items, false, func(r actions.Result) { ch <- r })
		close(ch)
	}()
	m.message = fmt.Sprintf("%s %d repositories…", op, len(items))
	return m, awaitBulkResult(op, ch, 0, 0)
}

func awaitBulkResult(op actions.BulkOp, ch <-chan actions.Result, succeeded, failed int) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return bulkDoneMsg{op: op, succeeded: succeeded, failed: failed}
		}
		return bulkResultMsg{op: op, result: r, ch: ch, succeeded: succeeded, failed: failed}
	}
}

func (m model) runRepair() (model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	m.items[m.cursor].busy = true
	m.message = fmt.Sprintf("repair: %s…", item.name())
	return m, func() tea.Msg {
		ok, msg, err := actions.ActionRepair(item.path)
		if err != nil {
			return actionDoneMsg{
				path:    item.path,
				message: fmt.Sprintf("repair %s failed: %v", item.name(), err),
				failed:  true,
			}
		}
		if !ok {
			return actionDoneMsg{
				path:    item.path,
				message: fmt.Sprintf("repair %s failed: %s", item.name(), msg),
				failed:  true,
			}
		}
		return actionDoneMsg{path: item.path, message: fmt.Sprintf("repair %s: %s", item.name(), msg)}
	}
}

func (m model) openEditor() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	args := append(parts[1:], item.path)
	m.message = fmt.Sprintf("open %s in %s", item.name(), parts[0])
	return m, tea.ExecProcess(exec.Command(parts[0], args...), func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{message: fmt.Sprintf("open %s failed: %v", item.name(), err), failed: true}
		}
		return actionDoneMsg{message: ""}
	})
}

func (m model) revealInFinder() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", item.path)
	case "windows":
		cmd = exec.Command("explorer", "/select,"+item.path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(item.path))
	}
	m.message = fmt.Sprintf("reveal %s", item.name())
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{message: fmt.Sprintf("reveal %s failed: %v", item.name(), err), failed: true}
		}
		return actionDoneMsg{message: ""}
	})
}

func refreshStatusCmd(path string) tea.Cmd {
	return loadStatusCmd(path)
}

func clearAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearMsg{}
	})
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading…"
	}
	if m.showDetail {
		return m.detail.View() + "\n(esc to return)"
	}

	var b strings.Builder
	// Theme-proof styling: no fixed foreground colors. The title is bold
	// against the default foreground, and selection is reverse video, which
	// is maximally contrasting on both dark and light terminals.
	titleStyle := lipgloss.NewStyle().Bold(true)
	totalStyle := lipgloss.NewStyle().Bold(true)
	selectedStyle := lipgloss.NewStyle().Reverse(true).Bold(true)
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F1C40F"})
	dimStyle := lipgloss.NewStyle().Faint(true)

	title := "gitsync: " + m.scanDir
	if len(m.items) > 0 {
		title += fmt.Sprintf("  [%d/%d]", m.cursor+1, len(m.items))
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString("No directories found.\n")
	} else {
		h := m.listHeight()
		end := m.offset + h
		if end > len(m.items) {
			end = len(m.items)
		}
		if m.offset > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", m.offset)))
			b.WriteString("\n")
		}
		prefixW := m.prefixWidth()
		for i := m.offset; i < end; i++ {
			item := m.items[i]
			line := strings.TrimRight(pad(m.rowPrefix(item), prefixW)+" "+item.statusText(), " ")
			if i == m.cursor {
				line = "> " + line
				// Pad the bar to the full width so the reversed selection
				// reads as a band. lipgloss.Width measures in cells, so wide
				// emoji badges don't push the line past the right edge.
				if pad := m.width - lipgloss.Width(line); pad > 0 {
					line += strings.Repeat(" ", pad)
				}
				line = selectedStyle.Render(line)
			} else {
				line = "  " + line
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		if end < len(m.items) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.items)-end)))
			b.WriteString("\n")
		}

		// A rule and a totals line close the table, standing in for the
		// scan root itself. The label sits in the name column so the
		// figures line up with the rows above.
		tot, complete := m.total()
		rule := strings.Repeat("─", min(m.width, 2+prefixW+1+40))
		b.WriteString(dimStyle.Render(rule))
		b.WriteString("\n")
		// The label sits in the name column so the figures line up under
		// the detail column of the rows above.
		label := pad("  "+strings.Repeat(" ", badgeSlots+1+typeIconWidth+1+2)+"Total", 2+prefixW)
		figures := tot.itemText(len(m.items))
		if !complete {
			figures += "…"
		}
		b.WriteString(totalStyle.Render(label + " " + figures))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	switch {
	case m.commitInput.Focused():
		b.WriteString(m.commitInput.View())
		b.WriteString("\n")
		hint := "enter to commit, esc to cancel"
		if m.drafting {
			hint = "drafting message… (editable now; enter to commit, esc to cancel)"
		}
		b.WriteString(dimStyle.Render(hint))
		b.WriteString("\n\n")
	case m.pending != nil:
		b.WriteString(msgStyle.Render(m.pending.prompt))
		b.WriteString("\n\n")
	case m.message != "":
		b.WriteString(msgStyle.Render(report.FirstLine(m.message)))
		b.WriteString("\n")
		if strings.Contains(m.message, "\n") {
			b.WriteString(dimStyle.Render("enter for full output"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(m.help.View(m.keys))
	return b.String()
}
