package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/osteele/gitsync/internal/actions"
)

func testChangeDescription(fileCount int) actions.ChangeDescription {
	files := make([]actions.DescriptionFile, 0, fileCount)
	for i := range fileCount {
		files = append(files, actions.DescriptionFile{Status: "modified", Path: fmt.Sprintf("src/file-%02d.go", i)})
	}
	return actions.ChangeDescription{
		Tool:           "jj-ai-commit",
		RequestedModel: "auto",
		ModelDisplay:   "mock/model",
		Target: actions.DescriptionTarget{
			Kind:    "working-copy",
			Display: "@",
			ID:      "abc123",
		},
		Description:   "feat: explain the structured view\n\nKeep the generated message visible.",
		Files:         files,
		DiffBytes:     493820,
		CommitMessage: "feat: explain the structured view\n\nKeep the generated message visible.",
	}
}

func TestDescriptionDetailCombinesSectionsWhenTheyFit(t *testing.T) {
	detail := newDescriptionDetail(testChangeDescription(2), 100, 40)
	if len(detail.pages) != 1 || detail.pages[0].kind != descriptionPageAll {
		t.Fatalf("expected one combined page, got %+v", detail.pages)
	}
	view := detail.view()
	for _, want := range []string{"Description", "Summary", "Files", "mock/model", "src/file-01.go"} {
		if !strings.Contains(view, want) {
			t.Fatalf("combined view missing %q:\n%s", want, view)
		}
	}
}

func TestDescriptionDetailSeparatesAndNavigatesLongSections(t *testing.T) {
	forceColorProfile(t)
	detail := newDescriptionDetail(testChangeDescription(30), 80, 10)
	if len(detail.pages) != 3 || detail.pages[0].kind != descriptionPageDescription {
		t.Fatalf("expected description, summary, and files pages, got %+v", detail.pages)
	}
	if view := detail.view(); strings.Contains(view, "src/file-29.go") || !strings.Contains(view, "structured view") {
		t.Fatalf("description page mixed in the long file list:\n%s", view)
	}
	header := strings.Split(detail.view(), "\n")[0]
	plainHeader := stripANSI(header)
	if !strings.Contains(plainHeader, "Description │ Summary │ Files") || strings.Contains(plainHeader, "1/3") {
		t.Fatalf("description header is not tab-like: %q", plainHeader)
	}
	if !strings.Contains(header, "\x1b[1mDescription") || !strings.Contains(header, "\x1b[2mSummary") {
		t.Fatalf("active and inactive tabs lack distinct weights: %q", header)
	}

	detail.nextPage(1)
	if !strings.Contains(stripANSI(detail.view()), "Changed files   30") {
		t.Fatalf("summary page missing counts:\n%s", detail.view())
	}
	detail.nextPage(1)
	if !strings.Contains(detail.view(), "src/file-00.go") {
		t.Fatalf("files page missing its first path:\n%s", detail.view())
	}
	detail.update(tea.KeyMsg{Type: tea.KeyPgDown})
	if detail.pages[detail.active].viewport.YOffset == 0 {
		t.Fatal("page-down did not scroll the files viewport")
	}
	if !strings.Contains(stripANSI(detail.view()), "•  model ·") {
		t.Fatalf("pinned model metadata disappeared:\n%s", detail.view())
	}

	detail.resize(100, 100)
	if len(detail.pages) != 1 || detail.pages[0].kind != descriptionPageAll {
		t.Fatalf("expected resize to combine sections, got %+v", detail.pages)
	}
}

func TestDescriptionDetailPagesLongDescription(t *testing.T) {
	description := testChangeDescription(1)
	description.Description = strings.Repeat("A wrapped description line. ", 80)
	detail := newDescriptionDetail(description, 50, 10)
	if detail.pages[0].kind != descriptionPageDescription {
		t.Fatalf("expected description page first, got %s", detail.pages[0].kind)
	}
	detail.update(tea.KeyMsg{Type: tea.KeyPgDown})
	if detail.pages[0].viewport.YOffset == 0 {
		t.Fatal("long description did not page")
	}
}

func TestDescriptionTypographyCompactsMetadata(t *testing.T) {
	description := testChangeDescription(1)
	description.ModelDisplay = "ai-commit-message: openrouter/openai/gpt-5.3-codex"
	description.Target.ID = "qlmxwyyxyxpsnmuwplkynztlnlyrxto"
	detail := newDescriptionDetail(description, 100, 40)
	view := detail.view()
	if !strings.Contains(view, "gpt-5.3-codex") || strings.Contains(strings.Split(view, "\n")[0], "openrouter") {
		t.Fatalf("header did not use the compact model name:\n%s", view)
	}
	if !strings.Contains(view, "qlmxwyyxyxps") || strings.Contains(view, description.Target.ID) {
		t.Fatalf("summary did not shorten the target ID:\n%s", view)
	}
	if strings.Contains(view, "────") {
		t.Fatalf("section headings still use text rules:\n%s", view)
	}
}

func TestDescriptionTypographyMovesSubjectSpacingAbove(t *testing.T) {
	styled := styleDescription("fix: subject\n\nBody text")
	plain := stripANSI(styled)
	if plain != "\nfix: subject\nBody text" {
		t.Fatalf("description spacing = %q", plain)
	}
}
