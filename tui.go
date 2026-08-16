package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// chromeHeight is the number of lines View always spends around the repo
// list: the title, the blank line under it, the blank line above the help,
// and the help line itself. Message blocks and scroll indicators are
// reserved on top of this, in listHeight.
const chromeHeight = 4

type tuiItem struct {
	path   string
	status *RepoStatus
	busy   bool
	// lastResult holds the full text of the most recent action result,
	// which is often multi-line command output worth reading in full.
	lastResult string
}

func (i tuiItem) name() string {
	return filepath.Base(i.path)
}

// icon renders the item's state as badges. Failure states stand alone;
// otherwise dirty and ahead/behind can appear together, since a repo is
// routinely both.
func (i tuiItem) icon() string {
	if i.busy {
		return "⏳"
	}
	if i.status == nil {
		return "⏱"
	}
	if i.status.Corrupted {
		return "❌"
	}
	if i.status.Error != "" {
		return "⚠️"
	}
	if i.status.Type == Bare {
		return "📁"
	}
	var badges string
	if i.status.Dirty {
		badges += "📝"
	}
	if i.status.Ahead.Positive() {
		badges += "⬆"
	}
	if i.status.Behind.Positive() {
		badges += "⬇"
	}
	if badges == "" {
		return "✅"
	}
	return badges
}

func (i tuiItem) statusText() string {
	if i.status == nil {
		return "loading…"
	}
	tokens := statusTokens(i.status)
	parts := make([]string, 0, len(tokens)+1)
	// Failure states lead with the error itself, not the repo type.
	if len(tokens) == 0 || (tokens[0].kind != stateError && tokens[0].kind != stateCorrupted) {
		parts = append(parts, i.status.Type.String())
	}
	for _, tok := range tokens {
		parts = append(parts, tok.text)
	}
	text := strings.Join(parts, " ")
	if i.busy {
		text += "…"
	}
	return text
}

type statusMsg struct {
	index  int
	status *RepoStatus
}

type actionDoneMsg struct {
	message string
	path    string
	failed  bool
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
	detail        viewport.Model
	showDetail    bool
}

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Push      key.Binding
	Pull      key.Binding
	Commit    key.Binding
	Sync      key.Binding
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
		{k.Up, k.Down, k.Push, k.Pull, k.Commit, k.Sync},
		{k.Repair, k.AddRemote, k.Open, k.Reveal, k.Detail, k.Quit},
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

func runTUI(scanDir string) error {
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
	subdirs, err := getSubdirectories(scanDir)
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
		cmds = append(cmds, loadStatusCmd(i, m.items[i].path))
	}
	return tea.Batch(cmds...)
}

func loadStatusCmd(index int, path string) tea.Cmd {
	return func() tea.Msg {
		status, err := getRepoStatus(path)
		if err != nil {
			// A scan error is not the same as corruption: keep whatever the
			// status detection produced (including its Corrupted verdict) and
			// surface the error without forcing the repair-dangerous state.
			if status == nil {
				status = &RepoStatus{Path: path, Type: Bare}
			}
			if status.Error == "" {
				status.Error = err.Error()
			}
		}
		return statusMsg{index: index, status: status}
	}
}

// listHeight is the number of repo rows that fit on screen, after
// reserving the lines View spends on chrome: the title, the separating
// blanks, the help line, whichever message block is showing, and the
// scroll indicators when the list does not fit.
func (m model) listHeight() int {
	reserved := chromeHeight
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
		if msg.index >= 0 && msg.index < len(m.items) {
			m.items[msg.index].status = msg.status
		}
		return m, nil

	case actionDoneMsg:
		m.message = msg.message
		idx := -1
		for i, item := range m.items {
			if item.path == msg.path {
				idx = i
				break
			}
		}
		var cmds []tea.Cmd
		if idx >= 0 {
			m.items[idx].busy = false
			m.items[idx].lastResult = msg.message
			cmds = append(cmds, refreshStatusCmd(idx, m.items[idx].path))
		}
		// Failures stay until the next keypress: their command output is
		// exactly what the user needs to read.
		m.messageSticky = msg.failed
		if !msg.failed {
			cmds = append(cmds, clearAfter(3*time.Second))
		}
		return m, tea.Batch(cmds...)

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

	case key.Matches(msg, m.keys.Detail):
		return m.showDetailView()

	case key.Matches(msg, m.keys.Push):
		return m.runAction("push", ActionPush)

	case key.Matches(msg, m.keys.Pull):
		return m.runAction("pull", ActionPull)

	case key.Matches(msg, m.keys.Commit):
		return m.startCommit()

	case key.Matches(msg, m.keys.Sync):
		return m.runAction("sync", ActionSync)

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
			message = defaultCommitMessage
		}
		m.commitInput.Blur()
		m.commitInput.SetValue("")
		return m.runAction("commit", func(path string) error {
			return ActionCommit(path, message)
		})
	case tea.KeyEsc, tea.KeyCtrlC:
		m.commitInput.Blur()
		m.commitInput.SetValue("")
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
	m.commitInput.SetValue(defaultCommitMessage)
	m.commitInput.CursorEnd()
	return m, m.commitInput.Focus()
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
	if !hasOrigin(item.path) {
		mm, cmd := m.runAction("add GitHub remote", ActionAddGitHubRemote)
		return mm, cmd
	}
	m.pending = &pendingAction{
		prompt: fmt.Sprintf("replace origin of %s with the GitHub remote? (y/n)", item.name()),
		run: func(m model) (model, tea.Cmd) {
			mm, cmd := m.runAction("add GitHub remote", ActionAddGitHubRemote)
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

func (m model) runRepair() (model, tea.Cmd) {
	item, ok := m.current()
	if !ok {
		return m, nil
	}
	m.items[m.cursor].busy = true
	m.message = fmt.Sprintf("repair: %s…", item.name())
	return m, func() tea.Msg {
		ok, msg, err := ActionRepair(item.path)
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

func refreshStatusCmd(index int, path string) tea.Cmd {
	return loadStatusCmd(index, path)
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
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00D8FF"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F1C40F"))
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
		for i := m.offset; i < end; i++ {
			item := m.items[i]
			line := fmt.Sprintf("%s %s  %s", item.icon(), item.name(), item.statusText())
			if i == m.cursor {
				line = cursorStyle.Render("> " + line)
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
	}

	b.WriteString("\n")
	switch {
	case m.commitInput.Focused():
		b.WriteString(m.commitInput.View())
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("enter to commit, esc to cancel"))
		b.WriteString("\n\n")
	case m.pending != nil:
		b.WriteString(msgStyle.Render(m.pending.prompt))
		b.WriteString("\n\n")
	case m.message != "":
		b.WriteString(msgStyle.Render(firstLine(m.message)))
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
