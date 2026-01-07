package llm

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultOllamaModel = "ministral-3:latest"
	// Context clipping guards assume ministral-3:latest exposes a 262k-token window (~1M characters).
	// We cap prompts well below that to keep >=20% headroom (roughly 4 chars/token) and avoid OOMs.
	maxSummaryChars        = 200_000
	maxAnswerChars         = 120_000
	maxSuggestionChars     = 150_000
	maxBriefChars          = 200_000
	maxBriefSummaryChars   = 60_000
	maxBriefTechnicalChars = 110_000
	maxBriefDeepDiveChars  = 40_000
)

const defaultLLMHTTPTimeout = 3 * time.Minute

// Config describes how to build an LLM client.
type Config struct {
	Model      string
	Endpoint   string
	HTTPClient *http.Client
}

// Client exposes summarization and question-answering helpers.
type Client interface {
	Summarize(ctx context.Context, title, content string) (string, error)
	Answer(ctx context.Context, title, question, content string) (string, error)
	SuggestNotes(ctx context.Context, title, abstract string, contributions []string, content string) ([]SuggestedNote, error)
	ReadingBrief(ctx context.Context, title, content string) (ReadingBrief, error)
	BriefSection(ctx context.Context, kind BriefSectionKind, title, content string) ([]string, error)
	StreamBriefSection(ctx context.Context, kind BriefSectionKind, title, content string, handler BriefSectionStreamHandler) error
	Name() string
}

// SuggestedNote is a structured response describing a potential zettelkasten entry.
type SuggestedNote struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Reason string `json:"reason"`
	Kind   string `json:"kind"`
}

// ReadingBrief captures the three-pass inspired sections rendered in the UI.
type ReadingBrief struct {
	Summary   []string `json:"summary"`
	Technical []string `json:"technical"`
	DeepDive  []string `json:"deepDive"`
}

// BriefSectionKind enumerates the supported three-pass brief sections.
type BriefSectionKind string

const (
	BriefSummary   BriefSectionKind = "summary"
	BriefTechnical BriefSectionKind = "technical"
	BriefDeepDive  BriefSectionKind = "deepDive"
)

// BriefSectionLimit reports the max character budget for the given section.
func BriefSectionLimit(kind BriefSectionKind) int {
	switch kind {
	case BriefSummary:
		return maxBriefSummaryChars
	case BriefTechnical:
		return maxBriefTechnicalChars
	case BriefDeepDive:
		return maxBriefDeepDiveChars
	default:
		return maxBriefChars
	}
}

// BriefSectionDelta captures streaming updates for a given section.
type BriefSectionDelta struct {
	Kind    BriefSectionKind
	Bullets []string
	Done    bool
}

// BriefSectionStreamHandler receives streaming updates as they are generated.
type BriefSectionStreamHandler func(delta BriefSectionDelta) error

// NewFromEnv inspects CLI arguments & environment variables to build a client.
func NewFromEnv(cfg Config) (Client, error) {
	host := cfg.Endpoint
	if host == "" {
		if env := os.Getenv("OLLAMA_HOST"); env != "" {
			host = strings.TrimRight(env, "/")
		} else {
			host = "http://localhost:11434"
		}
	}
	model := cfg.Model
	if model == "" {
		if env := os.Getenv("OLLAMA_MODEL"); env != "" {
			model = env
		} else {
			model = defaultOllamaModel
		}
	}
	return &ollamaClient{
		host:   host,
		model:  model,
		client: pickHTTPClient(cfg.HTTPClient),
	}, nil
}

func pickHTTPClient(custom *http.Client) *http.Client {
	if custom != nil {
		return custom
	}
	// Allow longer-running generations (Ollama often needs >60s) and rely on the caller's context for cancellation.
	return &http.Client{Timeout: defaultLLMHTTPTimeout}
}
