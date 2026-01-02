package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/csheth/browse/internal/llm"
	"github.com/csheth/browse/internal/tui"
)

func main() {
	defaultPath := filepath.Join(".", "zettelkasten.json")
	zettelPath := flag.String("zettel", defaultPath, "path to the knowledge base JSON file")
	noAltScreen := flag.Bool("no-alt-screen", false, "disable the alternate screen buffer")
	llmProvider := flag.String("llm-provider", "ollama", "LLM provider (auto, openai, ollama)")
	llmModel := flag.String("llm-model", "", "override the default model for the chosen provider")
	llmEndpoint := flag.String("llm-endpoint", "", "custom API base URL (OpenAI) or host (Ollama)")
	openAIKey := flag.String("openai-api-key", "", "OpenAI API key (overrides OPENAI_API_KEY)")
	flag.Parse()

	absPath, err := filepath.Abs(*zettelPath)
	if err != nil {
		fmt.Println("failed to resolve knowledge base path:", err)
		os.Exit(1)
	}

	var llmClient llm.Client
	llmClient, err = llm.NewFromEnv(llm.Config{
		Provider: llm.Provider(*llmProvider),
		Model:    *llmModel,
		Endpoint: *llmEndpoint,
		APIKey:   *openAIKey,
	})
	if err != nil {
		fmt.Println("LLM disabled:", err)
	}

	opts := []tea.ProgramOption{}
	if !*noAltScreen {
		opts = append(opts, tea.WithAltScreen())
	}
	opts = append(opts, tea.WithMouseCellMotion())
	program := tea.NewProgram(
		tui.New(tui.Config{
			KnowledgeBasePath: absPath,
			LLM:               llmClient,
		}),
		opts...,
	)

	if _, err := program.Run(); err != nil {
		fmt.Println("program error:", err)
		os.Exit(1)
	}
}
