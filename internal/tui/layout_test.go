package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"

	"github.com/csheth/browse/internal/llm"
)

func TestPageLayoutUpdate(t *testing.T) {
	cases := []struct {
		name             string
		width            int
		height           int
		viewportWidth    int
		viewportHeight   int
		transcriptHeight int
		composerHeight   int
	}{
		{name: "narrow", width: 80, height: 24, viewportWidth: 76, viewportHeight: 14, transcriptHeight: 6, composerHeight: 1},
		{name: "wide", width: 200, height: 40, viewportWidth: 196, viewportHeight: 30, transcriptHeight: 10, composerHeight: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newPageLayout()
			layout.Update(tc.width, tc.height)
			if layout.viewportWidth != tc.viewportWidth {
				t.Fatalf("viewport width mismatch: got %d want %d", layout.viewportWidth, tc.viewportWidth)
			}
			if layout.viewportHeight != tc.viewportHeight {
				t.Fatalf("viewport height mismatch: got %d want %d", layout.viewportHeight, tc.viewportHeight)
			}
			if layout.transcriptHeight != tc.transcriptHeight {
				t.Fatalf("transcript height mismatch: got %d want %d", layout.transcriptHeight, tc.transcriptHeight)
			}
			if layout.composerHeight != tc.composerHeight {
				t.Fatalf("composer height mismatch: got %d want %d", layout.composerHeight, tc.composerHeight)
			}
		})
	}
}

func TestFormatConversationEntryMarkdown(t *testing.T) {
	input := "**Bold** and *italic*\n- item one\n[Docs](https://example.com)"
	got := stripANSI(formatConversationEntry(input, 80))
	want := "Bold and italic\n• item one\nDocs (https://example.com)"
	if got != want {
		t.Fatalf("formatted output mismatch:\n%s", got)
	}
}

func TestFormatConversationEntryMarkdownBlocks(t *testing.T) {
	input := "### **Title**\n> quoted line\n`inline` and ~~strike~~\n| Col | Val |\n```go\nfunc main() {}\n```"
	got := stripANSI(formatConversationEntry(input, 20))
	want := "Title\nquoted line\ninline and strike\n| Col | Val |\nfunc main() {}"
	if got != want {
		t.Fatalf("formatted output mismatch:\n%s", got)
	}
}

func TestFormatConversationEntryNestedLists(t *testing.T) {
	input := "1. First item\n  - sub bullet\n    - deep bullet"
	got := stripANSI(formatConversationEntry(input, 80))
	want := "1. First item\n  • sub bullet\n    • deep bullet"
	if got != want {
		t.Fatalf("formatted output mismatch:\n%s", got)
	}
}

func TestFormatConversationEntryTableAlignment(t *testing.T) {
	input := "| Col | Val |\n| --- | ---- |\n| A | 1 |\n| Long | 22 |"
	got := stripANSI(formatConversationEntry(input, 80))
	want := strings.Join([]string{
		"| Col  | Val |",
		"| A    | 1   |",
		"| Long | 22  |",
	}, "\n")
	if got != want {
		t.Fatalf("formatted output mismatch:\n%s", got)
	}
}

func TestBuildDisplayContentStripsMarkdown(t *testing.T) {
	m := &model{
		viewport: viewport.New(80, 20),
		brief: llm.ReadingBrief{
			Summary: []string{"### **Title**", "| **Col** | **Val** |"},
		},
	}
	view := m.buildDisplayContent()
	content := stripANSI(view.content)
	if strings.Contains(content, "###") || strings.Contains(content, "**") {
		t.Fatalf("markdown markers should be stripped:\n%s", content)
	}
	if !strings.Contains(content, "Title") {
		t.Fatalf("expected title content in output:\n%s", content)
	}
	if !strings.Contains(content, "| Col | Val |") {
		t.Fatalf("expected table line in output:\n%s", content)
	}
}
