package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Provider represents a supported language model backend.
type Provider string

const (
	// ProviderAuto selects OpenAI when an API key is present, otherwise Ollama.
	ProviderAuto Provider = "auto"
	// ProviderOpenAI uses OpenAI's Chat Completions API.
	ProviderOpenAI Provider = "openai"
	// ProviderOllama targets a locally running Ollama daemon.
	ProviderOllama Provider = "ollama"

	defaultOpenAIModel = "gpt-4o-mini"
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
	Provider Provider
	Model    string
	Endpoint string
	APIKey   string
	// HTTPClient lets callers override the http.Client used for requests.
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
	provider := cfg.Provider
	if provider == "" || provider == ProviderAuto {
		if cfg.APIKey == "" {
			cfg.APIKey = os.Getenv("OPENAI_API_KEY")
		}
		if cfg.APIKey != "" {
			provider = ProviderOpenAI
		} else {
			provider = ProviderOllama
		}
	}

	switch provider {
	case ProviderOpenAI:
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, errors.New("OPENAI_API_KEY not set")
		}
		model := cfg.Model
		if model == "" {
			if env := os.Getenv("OPENAI_MODEL"); env != "" {
				model = env
			} else {
				model = defaultOpenAIModel
			}
		}
		baseURL := cfg.Endpoint
		if baseURL == "" {
			if env := os.Getenv("OPENAI_BASE_URL"); env != "" {
				baseURL = strings.TrimRight(env, "/")
			} else {
				baseURL = "https://api.openai.com/v1"
			}
		}
		return &openAIClient{
			apiKey: apiKey,
			model:  model,
			base:   baseURL,
			client: pickHTTPClient(cfg.HTTPClient),
		}, nil
	case ProviderOllama:
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
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider)
	}
}

func pickHTTPClient(custom *http.Client) *http.Client {
	if custom != nil {
		return custom
	}
	// Allow longer-running generations (Ollama often needs >60s) and rely on the caller's context for cancellation.
	return &http.Client{Timeout: defaultLLMHTTPTimeout}
}
