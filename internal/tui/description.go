package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/osteele/gitsync/internal/actions"
)

type descriptionPageKind string

const (
	descriptionPageAll         descriptionPageKind = "all"
	descriptionPageDescription descriptionPageKind = "description"
	descriptionPageSummary     descriptionPageKind = "summary"
	descriptionPageFiles       descriptionPageKind = "files"
)

type descriptionPage struct {
	kind     descriptionPageKind
	title    string
	viewport viewport.Model
}

// descriptionDetail owns the semantic document returned by an AI commit
// tool. Pages are derived from the document and terminal dimensions, so a
// resize can switch between one combined view and navigable section views.
type descriptionDetail struct {
	description actions.ChangeDescription
	pages       []descriptionPage
	active      int
	focus       descriptionPageKind
	width       int
	height      int
}

func newDescriptionDetail(description actions.ChangeDescription, width, height int) *descriptionDetail {
	detail := &descriptionDetail{
		description: description,
		focus:       descriptionPageDescription,
	}
	detail.resize(width, height)
	return detail
}

func (d *descriptionDetail) resize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 3 {
		height = 3
	}
	offsets := make(map[descriptionPageKind]int, len(d.pages))
	for _, page := range d.pages {
		offsets[page.kind] = page.viewport.YOffset
		if page.kind != descriptionPageAll && page.kind == d.activeKind() {
			d.focus = page.kind
		}
	}
	d.width = width
	d.height = height
	contentHeight := height - 2 // pinned metadata and action lines

	description := styleDescription(d.description.Description)
	summary := d.summaryContent()
	files := d.filesContent()
	all := section("Description", description) + "\n\n" + section("Summary", summary)
	if files != "" {
		all += "\n\n" + section("Files", files)
	}
	wrappedAll := wrapContent(all, width)
	if contentLineCount(wrappedAll) <= contentHeight {
		d.pages = []descriptionPage{newDescriptionPage(descriptionPageAll, "All", wrappedAll, width, contentHeight, offsets[descriptionPageAll])}
		d.active = 0
		return
	}

	pages := []descriptionPage{
		newDescriptionPage(descriptionPageDescription, "Description", wrapContent(description, width), width, contentHeight, offsets[descriptionPageDescription]),
		newDescriptionPage(descriptionPageSummary, "Summary", wrapContent(summary, width), width, contentHeight, offsets[descriptionPageSummary]),
	}
	if files != "" {
		pages = append(pages, newDescriptionPage(descriptionPageFiles, "Files", wrapContent(files, width), width, contentHeight, offsets[descriptionPageFiles]))
	}
	d.pages = pages
	d.active = 0
	for i, page := range d.pages {
		if page.kind == d.focus {
			d.active = i
			break
		}
	}
}

func newDescriptionPage(kind descriptionPageKind, title, content string, width, height, offset int) descriptionPage {
	vp := viewport.New(width, height)
	vp.SetContent(content)
	vp.SetYOffset(offset)
	return descriptionPage{kind: kind, title: title, viewport: vp}
}

func (d *descriptionDetail) activeKind() descriptionPageKind {
	if len(d.pages) == 0 || d.active < 0 || d.active >= len(d.pages) {
		return d.focus
	}
	return d.pages[d.active].kind
}

func (d *descriptionDetail) nextPage(delta int) {
	if len(d.pages) < 2 {
		return
	}
	d.active = (d.active + delta + len(d.pages)) % len(d.pages)
	d.focus = d.pages[d.active].kind
}

func (d *descriptionDetail) update(msg tea.KeyMsg) tea.Cmd {
	if len(d.pages) == 0 {
		return nil
	}
	var cmd tea.Cmd
	d.pages[d.active].viewport, cmd = d.pages[d.active].viewport.Update(msg)
	return cmd
}

func (d *descriptionDetail) view() string {
	if len(d.pages) == 0 {
		return ""
	}
	page := d.pages[d.active]
	metadata := []string{shortModelName(d.description.ModelDisplay), d.targetSummary()}
	metadata = append(metadata, fmt.Sprintf("%d files", len(d.description.Files)))
	if d.description.DiffBytes > 0 {
		metadata = append(metadata, formatByteCount(d.description.DiffBytes))
	}
	titleStyle := lipgloss.NewStyle().Bold(true)
	metadataStyle := lipgloss.NewStyle().Faint(true)
	header := titleStyle.Render(page.title)
	if len(d.pages) > 1 {
		var tabs strings.Builder
		for i, candidate := range d.pages {
			if i > 0 {
				tabs.WriteString(metadataStyle.Render(" │ "))
			}
			if i == d.active {
				tabs.WriteString(titleStyle.Render(candidate.title))
			} else {
				tabs.WriteString(metadataStyle.Render(candidate.title))
			}
		}
		header = tabs.String()
	}
	if rest := nonEmpty(metadata); len(rest) > 0 {
		header += metadataStyle.Render("  •  " + strings.Join(rest, " · "))
	}
	header = ansi.Truncate(header, d.width, "…")
	footer := "pgup/pgdn page • c commit and return • esc return"
	if len(d.pages) > 1 {
		footer = "←/→ view • " + footer
	}
	footer = metadataStyle.Render(ansi.Truncate(footer, d.width, "…"))
	return header + "\n" + page.viewport.View() + "\n" + footer
}

func (d *descriptionDetail) summaryContent() string {
	target := d.description.Target.Display
	if d.description.Target.ID != "" {
		target += " (" + shortenID(d.description.Target.ID) + ")"
	}
	rows := [][2]string{
		{"Target", target},
		{"Tool", d.description.Tool},
		{"Model", d.description.ModelDisplay},
		{"Changed files", fmt.Sprintf("%d%s", len(d.description.Files), d.fileCountSummary())},
		{"Diff input", formatByteCount(d.description.DiffBytes)},
	}
	if d.description.RequestedModel != "" && d.description.RequestedModel != d.description.ModelDisplay {
		rows = append(rows, [2]string{"Requested model", d.description.RequestedModel})
	}
	lines := make([]string, 0, len(rows)+3)
	labelStyle := lipgloss.NewStyle().Faint(true)
	for _, row := range rows {
		label := fmt.Sprintf("%-16s", row[0])
		lines = append(lines, labelStyle.Render(label)+row[1])
	}
	if previous := strings.TrimSpace(d.description.PreviousDescription); previous != "" {
		lines = append(lines, "", labelStyle.Render("Previous description"), previous)
	}
	return strings.Join(lines, "\n")
}

func (d *descriptionDetail) fileCountSummary() string {
	counts := make(map[string]int)
	for _, file := range d.description.Files {
		counts[file.Status]++
	}
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func (d *descriptionDetail) filesContent() string {
	lines := make([]string, 0, len(d.description.Files))
	statusStyle := lipgloss.NewStyle().Bold(true)
	for _, file := range d.description.Files {
		lines = append(lines, statusStyle.Render(fileStatusCode(file.Status))+"  "+file.Path)
	}
	return strings.Join(lines, "\n")
}

func (d *descriptionDetail) targetSummary() string {
	prefix := ""
	switch d.description.Tool {
	case "jj-ai-commit":
		prefix = "jj "
	case "git-ai-commit":
		prefix = "git "
	}
	return strings.TrimSpace(prefix + d.description.Target.Display)
}

func fileStatusCode(status string) string {
	switch status {
	case "added":
		return "A"
	case "modified":
		return "M"
	case "deleted":
		return "D"
	case "renamed":
		return "R"
	case "copied":
		return "C"
	default:
		return "?"
	}
}

func section(title, content string) string {
	return lipgloss.NewStyle().Bold(true).Render(title) + "\n" + content
}

func styleDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}
	firstLine, rest, found := strings.Cut(description, "\n")
	firstLine = lipgloss.NewStyle().Bold(true).Render(firstLine)
	if !found {
		return "\n" + firstLine
	}
	// Conventional commit messages usually put a blank line between the
	// subject and body. In the display, spend that separation above the bold
	// subject instead, so the description reads as one typographic unit.
	rest = strings.TrimPrefix(rest, "\n")
	return "\n" + firstLine + "\n" + rest
}

func shortModelName(model string) string {
	if _, rest, found := strings.Cut(model, ":"); found {
		model = strings.TrimSpace(rest)
	}
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

func shortenID(id string) string {
	const maxRunes = 12
	runes := []rune(id)
	if len(runes) <= maxRunes {
		return id
	}
	return string(runes[:maxRunes])
}

func wrapContent(content string, width int) string {
	return ansi.Wrap(content, width, "")
}

func contentLineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func formatByteCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1f MB", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0f KB", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
