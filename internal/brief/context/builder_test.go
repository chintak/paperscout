package context

import (
	"strings"
	"testing"

	"github.com/csheth/browse/internal/llm"
)

func TestBuilderDeduplicatesAndSkipsBoilerplate(t *testing.T) {
	builder := NewBuilder(nil)
	content := strings.Join([]string{
		"Abstract",
		"This is the abstract discussing the new method.",
		"References",
		"[1] Prior work with DOI:10.123/abc",
		"This is the abstract discussing the new method.",
		"The method relies on contrastive pretraining.",
	}, "\n\n")

	pkg := builder.Build(content)
	if len(pkg.Chunks) != 2 {
		t.Fatalf("expected 2 unique chunks, got %d", len(pkg.Chunks))
	}
	summary := pkg.Sections[llm.BriefSummary]
	if strings.Contains(summary, "References") || strings.Contains(summary, "DOI") {
		t.Fatalf("summary context should strip boilerplate, got %q", summary)
	}
	if strings.Count(summary, "abstract") != 1 {
		t.Fatalf("deduplication failed, got %q", summary)
	}
}

func TestBuilderRespectsBudgets(t *testing.T) {
	budgets := map[llm.BriefSectionKind]int{
		llm.BriefSummary:   30,
		llm.BriefTechnical: 40,
		llm.BriefDeepDive:  50,
	}
	builder := NewBuilder(budgets)
	content := strings.Repeat("This paragraph is somewhat long and detailed.\n\n", 5)
	pkg := builder.Build(content)
	for kind, limit := range budgets {
		got := len([]rune(pkg.Sections[kind]))
		if got > limit {
			t.Fatalf("%s exceeded budget: got %d want <= %d", kind, got, limit)
		}
	}
}
