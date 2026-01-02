package guide

import (
	"fmt"
	"strings"
)

// Step represents one actionable recommendation in the reading workflow.
type Step struct {
	Title       string
	Description string
}

// Metadata carries just enough context for personalizing guide steps.
type Metadata struct {
	Title   string
	Authors []string
}

// Build returns a three-pass inspired reading checklist tailored for a single paper.
func Build(meta Metadata) []Step {
	displayTitle := strings.TrimSpace(meta.Title)
	if displayTitle == "" {
		displayTitle = "the paper"
	}
	authors := ""
	if len(meta.Authors) > 0 {
		authors = fmt.Sprintf(" by %s", strings.Join(meta.Authors, ", "))
	}

	return []Step{
		{
			Title:       "Pass 1 – Quick skim",
			Description: fmt.Sprintf("Spend five minutes to answer: What domain is %s%s in? Note the venue, section structure, key figures, and unfamiliar terms for later lookup.", displayTitle, authors),
		},
		{
			Title:       "Pass 2 – Grasp the content",
			Description: "Dive into the core sections and redraw the main figure or diagram. Summarize the problem statement, method, and evaluation setup in your own words and flag any assumptions.",
		},
		{
			Title:       "Pass 3 – Deep audit",
			Description: "Follow derivations step by step, reproduce pseudo-code, and verify whether the conclusions follow. Identify limitations, replication hurdles, and extension ideas.",
		},
		{
			Title:       "Zettelkasten capture",
			Description: "Translate the insights into atomic notes: problem, approach, key results, and any cross-links to existing notes. Reference dataset, code URL, and follow-up questions.",
		},
		{
			Title:       "Creative prompts",
			Description: "List two ways the method could inspire your next project—think adaption to a new modality, combination with another technique, or simplifications worth testing.",
		},
	}
}
