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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tuiItem struct {
	path   string
	status *RepoStatus
}

func (i tuiItem) name() string {
	return filepath.Base(i.path)
}

func (i tuiItem) icon() string {
	if i.status == nil {
		return "⏱"
	}
	if i.status.Corrupted {
		return "❌"
	}
	switch i.status.Type {
	case Jujutsu:
		if i.status.Dirty {
			return "📝"
		}
		if i.status.Ahead {
			return "⬆"
		}
		if i.status.Behind {
			return "⬇"
		}
		return "✅"
	case Git:
		if i.status.Dirty {
			return "📝"
		}
		if i.status.Ahead {
			return "⬆"
		}
		if i.status.Behind {
			return "⬇"
		}
		return "✅"
	default:
		return "📁"
	}
}

func (i tuiItem) statusText() string {
	if i.status == nil {
		return "loading…"
	}
	if i.status.Corrupted {
		if i.status.Error != "" {
			return "corrupted: " + i.status.Error
		}
		return "corrupted"
	}
	parts := []string{i.status.Type.String()}
	if i.status.Dirty {
		parts = append(parts, "dirty")
	}
	if i.status.Ahead {
		parts = append(parts, "ahead")
	}
	if i.status.Behind {
		parts = append(parts, "behind")
	}
	if i.status.Remote {
		parts = append(parts, "remote")
	} else if i.status.Type != Bare {
		parts = append(parts, "local")
	}
	return strings.Join(parts, " ")
}

type statusMsg struct {
	index  int
	status *RepoStatus
}

type actionDoneMsg struct {
	message string
	path    string
}

type clearMsg struct{}

type model struct {
	scanDir string
	items   []tuiItem
	cursor  int
	message string
	width   int
	height  int
	help    help.Model
	keys    keyMap
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
	Quit      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Push, k.Pull, k.Commit, k.Sync, k.Repair, k.AddRemote, k.Open, k.Reveal, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Push, k.Pull, k.Commit, k.Sync},
		{k.Repair, k.AddRemote, k.Open, k.Reveal, k.Quit},
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
		key.WithHelp("u", "pull"),
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
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

func runTUI(scanDir string) error {
	if _, err := os.Stat(scanDir); err != nil {
		return fmt.Errorf("invalid scan directory: %w", err)
	}
	m := newModel(scanDir)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(scanDir string) model {
	subdirs, _ := getSubdirectories(scanDir)
	items := make([]tuiItem, 0, len(subdirs))
	for _, d := range subdirs {
		items = append(items, tuiItem{path: d})
	}
	return model{
		scanDir: scanDir,
		items:   items,
		help:    help.New(),
		keys:    defaultKeyMap,
	}
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
			status = &RepoStatus{
				Path:      path,
				Type:      Bare,
				Corrupted: true,
				Error:     err.Error(),
			}
		}
		return statusMsg{index: index, status: status}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
			return m, nil

		case key.Matches(msg, m.keys.Push):
			return m.runAction("push", ActionPush)

		case key.Matches(msg, m.keys.Pull):
			return m.runAction("pull", ActionPull)

		case key.Matches(msg, m.keys.Commit):
			return m.runAction("commit", func(path string) error {
				return ActionCommit(path, "")
			})

		case key.Matches(msg, m.keys.Sync):
			return m.runAction("sync", ActionSync)

		case key.Matches(msg, m.keys.Repair):
			return m.runRepair()

		case key.Matches(msg, m.keys.AddRemote):
			return m.runAction("add GitHub remote", ActionAddGitHubRemote)

		case key.Matches(msg, m.keys.Open):
			return m.openEditor()

		case key.Matches(msg, m.keys.Reveal):
			return m.revealInFinder()
		}

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
		cmds = append(cmds, clearAfter(3*time.Second))
		if idx >= 0 {
			cmds = append(cmds, refreshStatusCmd(idx, m.items[idx].path))
		}
		return m, tea.Batch(cmds...)

	case clearMsg:
		m.message = ""
		return m, nil
	}

	return m, nil
}

func (m model) runAction(name string, fn func(string) error) (model, tea.Cmd) {
	if len(m.items) == 0 {
		return m, nil
	}
	item := m.items[m.cursor]
	m.message = fmt.Sprintf("%s: %s…", name, item.name())
	return m, func() tea.Msg {
		if err := fn(item.path); err != nil {
			return actionDoneMsg{path: item.path, message: fmt.Sprintf("%s %s failed: %v", name, item.name(), err)}
		}
		return actionDoneMsg{path: item.path, message: fmt.Sprintf("%s %s done", name, item.name())}
	}
}

func (m model) runRepair() (model, tea.Cmd) {
	if len(m.items) == 0 {
		return m, nil
	}
	item := m.items[m.cursor]
	m.message = fmt.Sprintf("repair: %s…", item.name())
	return m, func() tea.Msg {
		ok, msg, err := ActionRepair(item.path)
		if err != nil {
			return actionDoneMsg{path: item.path, message: fmt.Sprintf("repair %s failed: %v", item.name(), err)}
		}
		if !ok {
			return actionDoneMsg{path: item.path, message: fmt.Sprintf("repair %s failed: %s", item.name(), msg)}
		}
		return actionDoneMsg{path: item.path, message: fmt.Sprintf("repair %s: %s", item.name(), msg)}
	}
}

func (m model) openEditor() (model, tea.Cmd) {
	if len(m.items) == 0 {
		return m, nil
	}
	item := m.items[m.cursor]
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
			return actionDoneMsg{message: fmt.Sprintf("open %s failed: %v", item.name(), err)}
		}
		return actionDoneMsg{message: ""}
	})
}

func (m model) revealInFinder() (model, tea.Cmd) {
	if len(m.items) == 0 {
		return m, nil
	}
	item := m.items[m.cursor]
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
			return actionDoneMsg{message: fmt.Sprintf("reveal %s failed: %v", item.name(), err)}
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

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00D8FF"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F1C40F"))
	b.WriteString(titleStyle.Render("gitsync: " + m.scanDir))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString("No directories found.\n")
	} else {
		for i, item := range m.items {
			line := fmt.Sprintf("%s %s  %s", item.icon(), item.name(), item.statusText())
			if i == m.cursor {
				line = cursorStyle.Render("> " + line)
			} else {
				line = "  " + line
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(msgStyle.Render(m.message))
		b.WriteString("\n\n")
	}
	b.WriteString(m.help.View(m.keys))
	return b.String()
}
