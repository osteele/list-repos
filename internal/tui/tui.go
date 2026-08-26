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
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	// rollupSeq rejects an older aggregate that finishes after an action has
	// already requested a newer scan for the same container.
	rollupSeq int
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
			// Every repository below a top-level container is nested relative
			// to the scan root, even when it is direct relative to that row.
			r.direct -= item.roll.direct
			r.nested += item.roll.direct
			continue
		}
		r = r.addAtDepth(item.status, 1)
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
	action  string
	failed  bool
}

type descriptionMsg struct {
	path        string
	description actions.ChangeDescription
	inputHash   string
	err         error
}

// bulkPlanMsg carries an asynchronous recursive scan for a bulk action. An
// empty scope means the uppercase scan-root action; otherwise scope is the
// directory selected for a lowercase rollup action.
type bulkPlanMsg struct {
	op       actions.BulkOp
	scope    string
	statuses []*vcs.RepoStatus
	err      error
}

// bulkResultMsg carries one finished repository from a running bulk batch;
// ch yields the rest. bulkDoneMsg ends the batch with the final tallies.
type bulkResultMsg struct {
	op               actions.BulkOp
	result           actions.Result
	ch               <-chan actions.Result
	paths            []string
	succeeded        int
	failed           int
	changedRevisions int
	changedRepos     int
	scope            string
	failures         []bulkFailure
}

type bulkDoneMsg struct {
	op               actions.BulkOp
	paths            []string
	succeeded        int
	failed           int
	changedRevisions int
	changedRepos     int
	scope            string
	failures         []bulkFailure
}

type bulkFailure struct {
	path     string
	repoType vcs.RepoType
	message  string
}

type bulkTickMsg struct {
	runID int
	at    time.Time
}

type bulkStartedMsg struct {
	runID int
	item  actions.PlanItem
	ch    <-chan actions.PlanItem
}

type bulkProgressState struct {
	runID          int
	op             actions.BulkOp
	total          int
	completed      int
	failed         int
	started        time.Time
	running        map[string]string
	completedPaths map[string]struct{}
}

// draftMsg carries a proposed commit message from the AI commit tool. seq
// identifies which prompt it belongs to: a draft still in flight when its
// prompt is submitted must not seed a later prompt on the same repository,
// whose diff it no longer describes.
type draftMsg struct {
	path        string
	seq         int
	message     string
	description actions.ChangeDescription
	inputHash   string
	err         error
}

type clearMsg struct{}

// pendingAction is a destructive action awaiting y/n confirmation.
type pendingAction struct {
	prompt string
	run    func(m model) (model, tea.Cmd)
}

type detailCommit struct {
	path    string
	message string
}

type cachedDescription struct {
	inputHash   string
	description actions.ChangeDescription
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
	commitInput   textarea.Model
	// drafting reports that a commit message is being generated for
	// draftPath; the prompt is already open and editable meanwhile. draftSeq
	// distinguishes the in-flight draft from any earlier one for the same
	// path.
	drafting        bool
	draftPath       string
	draftSeq        int
	detail          viewport.Model
	showDetail      bool
	descriptionView *descriptionDetail
	detailCommit    *detailCommit
	bulkRunID       int
	bulkProgress    *bulkProgressState
	// descriptionCache lives only as long as this TUI model. Entries are
	// repository-scoped and reused only while their VCS diff hash matches.
	descriptionCache map[string]cachedDescription
}

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Expand    key.Binding
	Collapse  key.Binding
	Push      key.Binding
	Pull      key.Binding
	Fix       key.Binding
	Commit    key.Binding
	Describe  key.Binding
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
	return []key.Binding{k.Up, k.Down, k.Describe, k.Commit, k.Sync, k.Detail, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Expand, k.Collapse, k.Push, k.Pull, k.Fix, k.Describe, k.Commit, k.Sync},
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
	Fix: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "jj fix"),
	),
	Commit: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "commit"),
	),
	Describe: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "describe changes"),
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
		key.WithKeys("v"),
		key.WithHelp("v", "reveal"),
	),
	Detail: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "details"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "keys"),
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
	input := textarea.New()
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetWidth(80)
	input.SetHeight(1)
	return model{
		scanDir:          scanDir,
		items:            items,
		help:             help.New(),
		keys:             defaultKeyMap,
		commitInput:      input,
		descriptionCache: make(map[string]cachedDescription),
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
				// Keep the detected type: an unreadable repository is still a
				// repository, and mislabeling it a directory invites
				// expanding it instead of showing which VCS failed.
				status = &vcs.RepoStatus{Path: path, Type: vcs.DetectRepoType(path)}
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
	repos   int
	direct  int
	nested  int
	dirty   int
	ahead   int
	behind  int
	unknown int
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
	// An undetermined ahead count is not zero — the repository may have
	// unpushed commits. Reporting it keeps the rollup from calling a
	// subtree synced when it was merely unverified.
	if s.Remote && !s.Ahead.Known {
		r.unknown++
	}
	return r
}

func (r rollup) addAtDepth(s *vcs.RepoStatus, depth int) rollup {
	if s.Type == vcs.Dir {
		return r
	}
	r = r.add(s)
	if depth <= 1 {
		r.direct++
	} else {
		r.nested++
	}
	return r
}

func (r rollup) plus(o rollup) rollup {
	return rollup{
		repos:   r.repos + o.repos,
		direct:  r.direct + o.direct,
		nested:  r.nested + o.nested,
		dirty:   r.dirty + o.dirty,
		ahead:   r.ahead + o.ahead,
		behind:  r.behind + o.behind,
		unknown: r.unknown + o.unknown,
	}
}

// text renders the rollup for a status column: the repository count plus
// only the states that are actually present, so a quiet subtree stays
// quiet.
func (r rollup) text() string {
	if r.repos == 0 {
		return ""
	}
	repoCount := fmt.Sprintf("%d %s", r.repos, plural(r.repos, "repo"))
	if r.nested > 0 && r.direct+r.nested == r.repos {
		repoCount += fmt.Sprintf(" (%d direct + %d nested)", r.direct, r.nested)
	}
	parts := []string{repoCount}
	if r.dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", r.dirty))
	}
	if r.ahead > 0 {
		parts = append(parts, fmt.Sprintf("%d ahead", r.ahead))
	}
	if r.behind > 0 {
		parts = append(parts, fmt.Sprintf("%d behind", r.behind))
	}
	if r.unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d ahead ?", r.unknown))
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

// rollupMsg carries a container's aggregated descendant state. A non-nil
// err means the subtree could not be read and the aggregate is unknown.
type rollupMsg struct {
	path string
	seq  int
	r    rollup
	err  error
}

// loadRollupCmd aggregates the state of every repository beneath a
// container. This is the expensive part of the view -- it scans the whole
// subtree -- so it runs as a background command per container and the row
// shows its plain count until the result lands.
func loadRollupCmd(path string, seq int) tea.Cmd {
	return func() tea.Msg {
		statuses, err := scan.Scan(path, scan.Options{Recursive: true, MaxDepth: rollupDepth})
		if err != nil {
			return rollupMsg{path: path, seq: seq, err: err}
		}
		var r rollup
		for _, s := range statuses {
			r = r.addAtDepth(s, s.Depth)
		}
		return rollupMsg{path: path, seq: seq, r: r}
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
		reserved += m.commitInput.Height() + 3
	case m.pending != nil:
		reserved += lipgloss.Height(m.wrappedPendingPrompt()) + 1
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
		m.resizeCommitInput()
		if m.descriptionView != nil {
			m.descriptionView.resize(msg.Width, msg.Height)
		}
		m = m.clampView()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case bulkTickMsg:
		if m.bulkProgress == nil || msg.runID != m.bulkProgress.runID {
			return m, nil
		}
		m.message = m.bulkProgress.message(msg.at)
		return m, bulkProgressTick(msg.runID)

	case bulkStartedMsg:
		if m.bulkProgress == nil || msg.runID != m.bulkProgress.runID {
			return m, nil
		}
		path := msg.item.Status.Path
		if _, done := m.bulkProgress.completedPaths[path]; !done {
			m.bulkProgress.running[path] = filepath.Base(path)
			m.message = m.bulkProgress.message(time.Now())
		}
		return m, awaitBulkStarted(msg.runID, msg.ch)

	case statusMsg:
		idx := m.findByPath(msg.path)
		if idx < 0 {
			return m, nil
		}
		m.items[idx].status = msg.status
		// A container's rollup can only start once its type is known.
		if msg.status.Type == vcs.Dir && !m.items[idx].rolledUp {
			m.items[idx].rollupSeq++
			return m, loadRollupCmd(msg.path, m.items[idx].rollupSeq)
		}
		return m, nil

	case rollupMsg:
		if idx := m.findByPath(msg.path); idx >= 0 && msg.seq == m.items[idx].rollupSeq && msg.err == nil {
			// A failed subtree scan leaves the row on its shallow count and
			// the total honestly incomplete; claiming an empty subtree that
			// was never read would hide the failure behind a quiet row.
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
		if !msg.failed && msg.action == "commit" {
			delete(m.descriptionCache, msg.path)
		}
		idx := m.findByPath(msg.path)
		var cmds []tea.Cmd
		if idx >= 0 {
			m.items[idx].busy = false
			m.items[idx].lastResult = msg.message
			cmds = append(cmds, refreshStatusCmd(m.items[idx].path))
		}
		cmds = append(cmds, m.invalidateAncestorRollups(msg.path)...)
		// Failures stay until the next keypress: their command output is
		// exactly what the user needs to read.
		m.messageSticky = msg.failed
		if !msg.failed {
			cmds = append(cmds, clearAfter(3*time.Second))
		}
		return m, tea.Batch(cmds...)

	case bulkPlanMsg:
		if msg.scope != "" {
			if idx := m.findByPath(msg.scope); idx >= 0 {
				m.items[idx].busy = false
			}
		}
		if msg.err != nil {
			scanPath := m.scanDir
			if msg.scope != "" {
				scanPath = msg.scope
			}
			m.message = fmt.Sprintf("scan %s failed: %v", filepath.Base(scanPath), msg.err)
			m.messageSticky = true
			return m, nil
		}
		return m.planBulk(msg.op, msg.statuses, msg.scope)

	case descriptionMsg:
		idx := m.findByPath(msg.path)
		if idx < 0 {
			return m, nil
		}
		m.items[idx].busy = false
		tool := actions.DescriptionToolFor(m.items[idx].status.Type)
		if msg.err != nil {
			m.items[idx].lastResult = fmt.Sprintf("%s failed: %v", tool, msg.err)
			m.message = m.items[idx].lastResult
			m.messageSticky = true
			return m, nil
		}
		m.message = ""
		m.items[idx].lastResult = fmt.Sprintf("%s proposed: %s", tool, msg.description.Description)
		if msg.inputHash != "" {
			if m.descriptionCache == nil {
				m.descriptionCache = make(map[string]cachedDescription)
			}
			m.descriptionCache[msg.path] = cachedDescription{inputHash: msg.inputHash, description: msg.description}
		}
		m.descriptionView = newDescriptionDetail(msg.description, m.width, m.height)
		m.detailCommit = &detailCommit{path: msg.path, message: msg.description.CommitMessage}
		return m, nil

	case bulkResultMsg:
		var cmds []tea.Cmd
		name := filepath.Base(msg.result.Item.Status.Path)
		var resultText string
		if msg.result.Err != nil {
			resultText = fmt.Sprintf("%s %s failed: %v", msg.op, name, msg.result.Err)
		} else {
			resultText = fmt.Sprintf("%s %s done", msg.op, name)
			if msg.op == actions.BulkFix {
				resultText += fmt.Sprintf(": %d %s changed", msg.result.ChangedRevisions, plural(msg.result.ChangedRevisions, "revision"))
			}
			if output := strings.TrimSpace(msg.result.Output); output != "" {
				resultText += "\n" + output
			}
		}
		if idx := m.findByPath(msg.result.Item.Status.Path); idx >= 0 {
			m.items[idx].busy = false
			m.items[idx].lastResult = resultText
			cmds = append(cmds, refreshStatusCmd(m.items[idx].path))
		}
		// A directory-scoped fix may target children that are not expanded.
		// Preserve each operation's review command on its visible ancestor
		// rollup so the result remains reachable with enter.
		if msg.op == actions.BulkFix && resultText != "" {
			for i := range m.items {
				if m.items[i].status == nil || m.items[i].status.Type != vcs.Dir || !pathWithin(m.items[i].path, msg.result.Item.Status.Path) {
					continue
				}
				if m.items[i].lastResult != "" {
					m.items[i].lastResult += "\n\n"
				}
				m.items[i].lastResult += resultText
			}
		}
		succeeded, failed := msg.succeeded, msg.failed
		changedRevisions, changedRepos := msg.changedRevisions, msg.changedRepos
		failures := msg.failures
		if msg.result.Err != nil {
			failed++
			failures = append(failures, bulkFailure{
				path:     msg.result.Item.Status.Path,
				repoType: msg.result.Item.Status.Type,
				message:  msg.result.Err.Error(),
			})
		} else {
			succeeded++
			if msg.op == actions.BulkFix && msg.result.ChangedRevisions > 0 {
				changedRevisions += msg.result.ChangedRevisions
				changedRepos++
			}
			if msg.op == actions.BulkCommit {
				delete(m.descriptionCache, msg.result.Item.Status.Path)
			}
		}
		if m.bulkProgress != nil {
			m.bulkProgress.completed = succeeded + failed
			m.bulkProgress.failed = failed
			path := msg.result.Item.Status.Path
			m.bulkProgress.completedPaths[path] = struct{}{}
			delete(m.bulkProgress.running, path)
			m.message = m.bulkProgress.message(time.Now())
		}
		cmds = append(cmds, awaitBulkResult(msg.op, msg.ch, msg.paths, msg.scope, succeeded, failed, changedRevisions, changedRepos, failures))
		return m, tea.Batch(cmds...)

	case bulkDoneMsg:
		m.bulkProgress = nil
		summary := bulkSummary(msg.op, msg.succeeded, msg.failed)
		if msg.op == actions.BulkFix {
			summary = fixSummary(msg.changedRevisions, msg.changedRepos, msg.failed)
		}
		report := bulkFailureReport(summary, msg.failures)
		m.message = report
		m.messageSticky = msg.failed > 0
		if msg.scope != "" {
			if idx := m.findByPath(msg.scope); idx >= 0 {
				m.items[idx].busy = false
				m.items[idx].lastResult = report
			}
		}
		cmds := m.invalidateAncestorRollups(msg.paths...)
		if len(msg.failures) > 0 {
			updated, _ := m.openDetailContent(report, nil)
			m = updated.(model)
		}
		if msg.failed == 0 {
			cmds = append(cmds, clearAfter(3*time.Second))
		}
		return m, tea.Batch(cmds...)

	case draftMsg:
		if msg.path != m.draftPath || msg.seq != m.draftSeq {
			return m, nil // the prompt moved on; the draft is stale
		}
		m.drafting = false
		if msg.err != nil || strings.TrimSpace(msg.message) == "" {
			return m, nil // keep the canned default; the prompt still works
		}
		if msg.inputHash != "" && msg.description.CommitMessage != "" {
			if m.descriptionCache == nil {
				m.descriptionCache = make(map[string]cachedDescription)
			}
			m.descriptionCache[msg.path] = cachedDescription{
				inputHash:   msg.inputHash,
				description: msg.description,
			}
		}
		// Only seed an untouched prompt: replacing text the user has begun
		// editing would be worse than offering no draft at all.
		if m.commitInput.Focused() && m.commitInput.Value() == actions.DefaultCommitMessage {
			m.commitInput.SetValue(msg.message)
			m.commitInput.CursorEnd()
			m.resizeCommitInput()
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
	if m.descriptionView != nil {
		switch msg.String() {
		case "esc", "q":
			m.descriptionView = nil
			m.detailCommit = nil
			return m, nil
		case "left", "h", "shift+tab":
			m.descriptionView.nextPage(-1)
			return m, nil
		case "right", "l", "tab":
			m.descriptionView.nextPage(1)
			return m, nil
		case "c":
			if m.detailCommit != nil {
				commit := *m.detailCommit
				m.descriptionView = nil
				m.detailCommit = nil
				return m.runActionAt("commit", commit.path, func(path string) error {
					return actions.ActionCommit(path, commit.message)
				})
			}
		}
		return m, m.descriptionView.update(msg)
	}
	if m.showDetail {
		switch msg.String() {
		case "esc", "q", "enter":
			m.showDetail = false
			m.detailCommit = nil
			return m, nil
		case "c":
			if m.detailCommit != nil {
				commit := *m.detailCommit
				m.showDetail = false
				m.detailCommit = nil
				return m.runActionAt("commit", commit.path, func(path string) error {
					return actions.ActionCommit(path, commit.message)
				})
			}
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}

	// Help is a modal overlay: only its close keys act while it is visible.
	if m.help.ShowAll {
		if msg.String() == "esc" || key.Matches(msg, m.keys.Help) {
			m.help.ShowAll = false
		}
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
		return m.runRepositoryAction(actions.BulkPush, "push", actions.ActionPush)

	case key.Matches(msg, m.keys.Pull):
		return m.runRepositoryAction(actions.BulkPull, "pull", actions.ActionPull)

	case key.Matches(msg, m.keys.Fix):
		return m.startFix()

	case key.Matches(msg, m.keys.Describe):
		return m.startDescription()

	case key.Matches(msg, m.keys.Commit):
		return m.startCommit()

	case key.Matches(msg, m.keys.Sync):
		return m.runRepositoryAction(actions.BulkSync, "sync", actions.ActionSync)

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
		m.draftSeq++
		return m.runAction("commit", func(path string) error {
			return actions.ActionCommit(path, message)
		})
	case tea.KeyEsc, tea.KeyCtrlC:
		m.commitInput.Blur()
		m.commitInput.SetValue("")
		m.drafting, m.draftPath = false, ""
		m.draftSeq++
		m.message = "commit cancelled"
		return m, clearAfter(2 * time.Second)
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	m.resizeCommitInput()
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
	if vcs.DetectRepoType(item.path) == vcs.Dir {
		return m.startScopedBulk(actions.BulkCommit, item.path)
	}
	m.commitInput.SetValue(actions.DefaultCommitMessage)
	m.commitInput.CursorEnd()
	m.resizeCommitInput()
	cmds := []tea.Cmd{m.commitInput.Focus()}
	// Drafting reaches an LLM and takes seconds, so the prompt opens
	// immediately on the canned message and the draft replaces it when it
	// lands. Typing is never blocked, and never interrupted -- the draft is
	// dropped if the user has already started editing.
	cached, hasCached := m.descriptionCache[item.path]
	if shouldDraftCommitMessage(item.status) && (hasCached || actions.CommitToolAvailable(item.path)) {
		m.drafting = true
		m.draftPath = item.path
		m.draftSeq++
		cmds = append(cmds, draftCommitCmd(item.path, m.draftSeq, cached, hasCached))
	}
	return m, tea.Batch(cmds...)
}

func (m model) startFix() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	if item.status == nil {
		m.message = fmt.Sprintf("%s: status is still loading", item.name())
		return m, clearAfter(2 * time.Second)
	}
	if item.status.Type == vcs.Dir {
		return m.startScopedBulk(actions.BulkFix, item.path)
	}
	if item.status.Type != vcs.Jujutsu {
		m.message = fmt.Sprintf("%s: jj fix requires a Jujutsu repository", item.name())
		return m, clearAfter(3 * time.Second)
	}
	return m.runFixAt(item.path)
}

func (m model) startDescription() (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	if item.status == nil {
		m.message = fmt.Sprintf("%s: status is still loading", item.name())
		return m, clearAfter(2 * time.Second)
	}
	if item.status.Type == vcs.Dir {
		m.message = fmt.Sprintf("%s: not a repository", item.name())
		return m, clearAfter(2 * time.Second)
	}
	if item.status.Error != "" || item.status.Corrupted {
		m.message = fmt.Sprintf("%s: repository status is unavailable", item.name())
		return m, clearAfter(2 * time.Second)
	}
	if !item.status.Dirty {
		m.message = fmt.Sprintf("%s: no dirty changes to describe", item.name())
		return m, clearAfter(2 * time.Second)
	}
	tool := actions.DescriptionToolFor(item.status.Type)
	if !actions.DescriptionToolAvailable(item.path) {
		m.message = fmt.Sprintf("%s is not installed", tool)
		return m, clearAfter(3 * time.Second)
	}
	m.items[m.cursor].busy = true
	m.message = fmt.Sprintf("describing changes in %s…", item.name())
	cached, hasCached := m.descriptionCache[item.path]
	return m, describeChangesCmd(item.path, cached, hasCached)
}

func describeChangesCmd(path string, cached cachedDescription, hasCached bool) tea.Cmd {
	return func() tea.Msg {
		inputHash, hashErr := actions.DescriptionInputHash(path)
		if hashErr == nil && hasCached && cached.inputHash == inputHash {
			return descriptionMsg{path: path, description: cached.description, inputHash: inputHash}
		}
		description, err := actions.DescribeChanges(path)
		return descriptionMsg{
			path:        path,
			description: description,
			inputHash:   inputHash,
			err:         err,
		}
	}
}

// shouldDraftCommitMessage reports whether a row deserves an AI draft: the
// tool reads the diff, so there must be one, and a corrupted repository
// cannot be committed no matter what the message says.
func shouldDraftCommitMessage(status *vcs.RepoStatus) bool {
	return status != nil && status.Dirty && !status.Corrupted
}

// draftCommitCmd asks the AI commit tool for a proposed message. seq ties
// the result to the prompt that requested it.
func draftCommitCmd(path string, seq int, cached cachedDescription, hasCached bool) tea.Cmd {
	return func() tea.Msg {
		inputHash, _ := actions.DescriptionInputHash(path)
		if inputHash != "" && hasCached && cached.inputHash == inputHash {
			return draftMsg{
				path:        path,
				seq:         seq,
				message:     cached.description.CommitMessage,
				description: cached.description,
				inputHash:   inputHash,
			}
		}
		description, err := actions.DescribeChanges(path)
		return draftMsg{
			path:        path,
			seq:         seq,
			message:     description.CommitMessage,
			description: description,
			inputHash:   inputHash,
			err:         err,
		}
	}
}

// resizeCommitInput expands the editor to show the complete message when it
// fits while preserving at least one repository row and the surrounding TUI
// chrome. Longer drafts remain scrollable inside the editor.
func (m *model) resizeCommitInput() {
	width := m.width
	if width < 1 {
		width = 80
	}
	m.commitInput.SetWidth(width)
	height := contentLineCount(wrapContent(m.commitInput.Value(), width))
	if height < 1 {
		height = 1
	}
	maxHeight := m.height - 12
	if maxHeight < 1 {
		maxHeight = 1
	}
	if height > maxHeight {
		height = maxHeight
	}
	m.commitInput.SetHeight(height)
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
	return m.openDetailContent(b.String(), nil)
}

func (m model) openDetailContent(content string, commit *detailCommit) (tea.Model, tea.Cmd) {
	height := m.height - 2
	if height < 1 {
		height = 1
	}
	m.detail = viewport.New(m.width, height)
	m.detail.SetContent(content)
	m.showDetail = true
	m.detailCommit = commit
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
		prompt: fmt.Sprintf("Repair %s? This moves .git aside. (y/n)", item.name()),
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
		prompt: fmt.Sprintf("Replace origin of %s with the GitHub remote? (y/n)", item.name()),
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
	return m.runActionAt(name, item.path, fn)
}

func (m model) runRepositoryAction(op actions.BulkOp, name string, fn func(string) error) (tea.Model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	if item.busy {
		return m.busyMessage(item)
	}
	if item.status == nil {
		m.message = fmt.Sprintf("%s: status is still loading", item.name())
		return m, clearAfter(2 * time.Second)
	}
	if item.status.Type == vcs.Dir {
		return m.startScopedBulk(op, item.path)
	}
	return m.runActionAt(name, item.path, fn)
}

func (m model) runActionAt(name, path string, fn func(string) error) (tea.Model, tea.Cmd) {
	return m.runOutputActionAt(name, path, func(path string) (string, error) {
		return "", fn(path)
	})
}

func (m model) runOutputActionAt(name, path string, fn func(string) (string, error)) (tea.Model, tea.Cmd) {
	idx := m.findByPath(path)
	if idx < 0 {
		m.message = fmt.Sprintf("%s: repository is no longer visible", filepath.Base(path))
		return m, clearAfter(2 * time.Second)
	}
	item := m.items[idx]
	if item.busy {
		return m.busyMessage(item)
	}
	m.items[idx].busy = true
	m.message = fmt.Sprintf("%s: %s…", name, item.name())
	return m, func() tea.Msg {
		output, err := fn(item.path)
		if err != nil {
			return actionDoneMsg{
				path:    item.path,
				action:  name,
				message: fmt.Sprintf("%s %s failed: %v", name, item.name(), err),
				failed:  true,
			}
		}
		message := fmt.Sprintf("%s %s done", name, item.name())
		if output = strings.TrimSpace(output); output != "" {
			message += "\n" + output
		}
		return actionDoneMsg{path: item.path, action: name, message: message}
	}
}

func (m model) runFixAt(path string) (tea.Model, tea.Cmd) {
	idx := m.findByPath(path)
	if idx < 0 {
		return m, nil
	}
	item := m.items[idx]
	m.items[idx].busy = true
	m.message = fmt.Sprintf("fix: %s…", item.name())
	return m, func() tea.Msg {
		result, err := actions.ActionFix(item.path)
		if err != nil {
			return actionDoneMsg{
				path:    item.path,
				action:  "fix",
				message: fmt.Sprintf("fix %s failed: %v", item.name(), err),
				failed:  true,
			}
		}
		message := fmt.Sprintf("fix %s done: %d %s changed", item.name(), result.ChangedRevisions, plural(result.ChangedRevisions, "revision"))
		if output := strings.TrimSpace(result.Output); output != "" {
			message += "\n" + output
		}
		return actionDoneMsg{path: item.path, action: "fix", message: message}
	}
}

// invalidateAncestorRollups marks every visible container that owns one of
// paths as pending and schedules a fresh aggregate. The sequence prevents a
// slower pre-action scan from overwriting the post-action result.
func (m *model) invalidateAncestorRollups(paths ...string) []tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.items {
		item := &m.items[i]
		if item.status == nil || item.status.Type != vcs.Dir {
			continue
		}
		for _, path := range paths {
			if !pathWithin(item.path, path) {
				continue
			}
			item.rolledUp = false
			item.rollupSeq++
			cmds = append(cmds, loadRollupCmd(item.path, item.rollupSeq))
			break
		}
	}
	return cmds
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// startBulk scans the whole recursive scope in the background before planning.
// Uppercase actions mean all repositories represented by the display, not only
// repository rows that happen to be expanded at the moment.
func (m model) startBulk(op actions.BulkOp) (tea.Model, tea.Cmd) {
	m.message = fmt.Sprintf("planning %s for all repositories…", op)
	return m, func() tea.Msg {
		statuses, err := scan.Scan(m.scanDir, scan.Options{Recursive: true, MaxDepth: rollupDepth})
		return bulkPlanMsg{op: op, statuses: statuses, err: err}
	}
}

// startScopedBulk applies a lowercase action to the selected row's rollup:
// recursively scan that directory, then use the normal eligibility and
// confirmation flow only for the repositories it represents.
func (m model) startScopedBulk(op actions.BulkOp, scope string) (tea.Model, tea.Cmd) {
	idx := m.findByPath(scope)
	if idx < 0 {
		return m, nil
	}
	m.items[idx].busy = true
	m.items[idx].lastResult = ""
	m.message = fmt.Sprintf("planning %s in %s…", op, filepath.Base(scope))
	return m, func() tea.Msg {
		statuses, err := scan.Scan(scope, scan.Options{Recursive: true, MaxDepth: rollupDepth})
		return bulkPlanMsg{op: op, scope: scope, statuses: statuses, err: err}
	}
}

func (m model) planBulk(op actions.BulkOp, statuses []*vcs.RepoStatus, scope string) (tea.Model, tea.Cmd) {
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
	scopeText := ""
	if scope != "" {
		scopeText = " in " + filepath.Base(scope)
	}
	m.pending = &pendingAction{
		prompt: fmt.Sprintf("%s %d %s%s (%s)? (y/n)", bulkPromptVerb(op), len(items), noun, scopeText, strings.Join(names, ", ")),
		run:    func(m model) (model, tea.Cmd) { return m.runBulk(op, items, scope) },
	}
	return m, nil
}

func bulkPromptVerb(op actions.BulkOp) string {
	switch op {
	case actions.BulkCommit:
		return "Commit"
	case actions.BulkPull:
		return "Pull"
	case actions.BulkFix:
		return "Fix"
	case actions.BulkPush:
		return "Push"
	case actions.BulkSync:
		return "Sync"
	default:
		return "Run"
	}
}

// runBulk marks every planned repository busy and starts the batch. The
// engine runs in a goroutine feeding a channel; awaitBulkResult turns each
// result into a message so rows refresh as they finish.
func (m model) runBulk(op actions.BulkOp, items []actions.PlanItem, scope string) (model, tea.Cmd) {
	paths := make([]string, 0, len(items))
	if scope != "" {
		if idx := m.findByPath(scope); idx >= 0 {
			m.items[idx].busy = true
		}
	}
	for _, item := range items {
		paths = append(paths, item.Status.Path)
		if idx := m.findByPath(item.Status.Path); idx >= 0 {
			m.items[idx].busy = true
		}
	}
	ch := make(chan actions.Result, len(items))
	startedCh := make(chan actions.PlanItem, len(items))
	go func() {
		actions.ExecuteBulkWithProgress(op, items, false, func(item actions.PlanItem) { startedCh <- item }, func(r actions.Result) { ch <- r })
		close(startedCh)
		close(ch)
	}()
	m.bulkRunID++
	m.bulkProgress = &bulkProgressState{
		runID:          m.bulkRunID,
		op:             op,
		total:          len(items),
		started:        time.Now(),
		running:        make(map[string]string),
		completedPaths: make(map[string]struct{}),
	}
	m.message = m.bulkProgress.message(m.bulkProgress.started)
	return m, tea.Batch(
		awaitBulkResult(op, ch, paths, scope, 0, 0, 0, 0, nil),
		awaitBulkStarted(m.bulkProgress.runID, startedCh),
		bulkProgressTick(m.bulkProgress.runID),
	)
}

func awaitBulkResult(op actions.BulkOp, ch <-chan actions.Result, paths []string, scope string, succeeded, failed, changedRevisions, changedRepos int, failures []bulkFailure) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return bulkDoneMsg{op: op, paths: paths, scope: scope, succeeded: succeeded, failed: failed, changedRevisions: changedRevisions, changedRepos: changedRepos, failures: failures}
		}
		return bulkResultMsg{op: op, result: r, ch: ch, paths: paths, scope: scope, succeeded: succeeded, failed: failed, changedRevisions: changedRevisions, changedRepos: changedRepos, failures: failures}
	}
}

func bulkFailureReport(summary string, failures []bulkFailure) string {
	if len(failures) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(summary)
	b.WriteString("\n\nFailures")
	for i, failure := range failures {
		fmt.Fprintf(&b, "\n\nFailure %d of %d\n", i+1, len(failures))
		fmt.Fprintf(&b, "Repository: %s\n", filepath.Base(failure.path))
		fmt.Fprintf(&b, "Location: %s\n", displayPath(failure.path))
		fmt.Fprintf(&b, "Version control: %s\n", repoTypeName(failure.repoType))
		b.WriteString("\nError:\n")
		b.WriteString(strings.TrimSpace(failure.message))
	}
	return b.String()
}

func bulkSummary(op actions.BulkOp, succeeded, failed int) string {
	succeededNoun := "repositories"
	if succeeded == 1 {
		succeededNoun = "repository"
	}
	if failed == 0 {
		return fmt.Sprintf("%s: %d %s succeeded", bulkPromptVerb(op), succeeded, succeededNoun)
	}
	return fmt.Sprintf("%s: %d %s succeeded; %d failed", bulkPromptVerb(op), succeeded, succeededNoun, failed)
}

func repoTypeName(repoType vcs.RepoType) string {
	switch repoType {
	case vcs.Git:
		return "Git"
	case vcs.Jujutsu:
		return "Jujutsu"
	default:
		return repoType.String()
	}
}

func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

func (p bulkProgressState) message(now time.Time) string {
	noun := "repositories"
	if p.total == 1 {
		noun = "repository"
	}
	elapsed := now.Sub(p.started).Truncate(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	message := fmt.Sprintf("%s %d %s… %d/%d complete", bulkActiveVerb(p.op), p.total, noun, p.completed, p.total)
	if p.failed > 0 {
		message += fmt.Sprintf(", %d failed", p.failed)
	}
	running := make([]string, 0, len(p.running))
	for _, name := range p.running {
		running = append(running, name)
	}
	sort.Strings(running)
	if len(running) > 0 {
		const shown = 2
		names := running
		if len(names) > shown {
			names = names[:shown]
		}
		message += fmt.Sprintf(" · %d running: %s", len(running), strings.Join(names, ", "))
		if len(running) > shown {
			message += fmt.Sprintf(" +%d", len(running)-shown)
		}
	}
	queued := p.total - p.completed - len(p.running)
	if queued > 0 {
		message += fmt.Sprintf(" · %d queued", queued)
	}
	return fmt.Sprintf("%s · %s", message, elapsed)
}

func bulkActiveVerb(op actions.BulkOp) string {
	switch op {
	case actions.BulkCommit:
		return "Committing"
	case actions.BulkPull:
		return "Pulling"
	case actions.BulkFix:
		return "Fixing"
	case actions.BulkPush:
		return "Pushing"
	case actions.BulkSync:
		return "Syncing"
	default:
		return "Running"
	}
}

func bulkProgressTick(runID int) tea.Cmd {
	return tea.Tick(time.Second, func(at time.Time) tea.Msg {
		return bulkTickMsg{runID: runID, at: at}
	})
}

func awaitBulkStarted(runID int, ch <-chan actions.PlanItem) tea.Cmd {
	return func() tea.Msg {
		item, ok := <-ch
		if !ok {
			return nil
		}
		return bulkStartedMsg{runID: runID, item: item, ch: ch}
	}
}

func fixSummary(revisions, repos, failed int) string {
	repositoryNoun := "repositories"
	if repos == 1 {
		repositoryNoun = "repository"
	}
	return fmt.Sprintf("Fix: %d %s changed across %d %s; %d failed",
		revisions, plural(revisions, "revision"), repos, repositoryNoun, failed)
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
	if m.descriptionView != nil {
		return m.descriptionView.view()
	}
	if m.showDetail {
		hint := "(esc to return)"
		if m.detailCommit != nil {
			hint = "(esc to return, c to commit and return)"
		}
		return m.detail.View() + "\n" + hint
	}
	if m.help.ShowAll {
		return m.helpOverlay()
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
		b.WriteString(titleStyle.Render("Commit message"))
		b.WriteString("\n")
		b.WriteString(m.commitInput.View())
		b.WriteString("\n")
		hint := "enter to commit, esc to cancel"
		if m.drafting {
			hint = "drafting message… (editable now; enter to commit, esc to cancel)"
		}
		b.WriteString(dimStyle.Render(hint))
		b.WriteString("\n\n")
	case m.pending != nil:
		b.WriteString(msgStyle.Render(m.wrappedPendingPrompt()))
		b.WriteString("\n\n")
	case m.message != "":
		b.WriteString(msgStyle.Render(ansi.Truncate(report.FirstLine(m.message), m.width, "…")))
		b.WriteString("\n")
		if strings.Contains(m.message, "\n") {
			b.WriteString(dimStyle.Render("enter for full output"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return anchorFooter(b.String(), m.help.View(m.keys), m.height)
}

func (m model) wrappedPendingPrompt() string {
	if m.pending == nil {
		return ""
	}
	return ansi.Wrap(m.pending.prompt, max(1, m.width), ",")
}

// anchorFooter fills otherwise-unused terminal rows above the short-help line.
// Directory expansion therefore changes the table, not the footer's screen
// position. The list-height calculation already reserves a blank line between
// content and the footer, including when the terminal is full.
func anchorFooter(content, footer string, height int) string {
	content = strings.TrimRight(content, "\n")
	footer = strings.TrimRight(footer, "\n")
	gap := height - lipgloss.Height(content) - lipgloss.Height(footer) + 1
	if gap < 2 {
		gap = 2
	}
	return content + strings.Repeat("\n", gap) + footer
}

func (m model) helpOverlay() string {
	modalWidth := m.width - 4
	if modalWidth < 1 {
		modalWidth = 1
	}
	if modalWidth > 78 {
		modalWidth = 78
	}
	h := m.help
	h.Width = max(1, modalWidth-6)
	title := lipgloss.NewStyle().Bold(true).Render("Keyboard Shortcuts")
	hint := lipgloss.NewStyle().Faint(true).Render("Press ? or esc to close")
	body := title + "\n\n" + h.View(m.keys) + "\n\n" + hint
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#5B5BD6", Dark: "#8C8CFF"}).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
