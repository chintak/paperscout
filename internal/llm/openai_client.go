package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type openAIClient struct {
	apiKey string
	model  string
	base   string
	client *http.Client
}

func (c *openAIClient) Name() string {
	return fmt.Sprintf("OpenAI (%s)", c.model)
}

func (c *openAIClient) Summarize(ctx context.Context, title, content string) (string, error) {
	context := clipText(content, maxSummaryChars)
	if context == "" {
		return "", fmt.Errorf("paper text empty; cannot summarize")
	}
	prompt := buildSummaryPrompt(title, context)
	return c.chat(ctx, prompt)
}

func (c *openAIClient) Answer(ctx context.Context, title, question, content string) (string, error) {
	if strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("question cannot be empty")
	}
	context := extractQuestionContext(content, question, maxAnswerChars)
	if context == "" {
		return "", fmt.Errorf("paper text empty; cannot answer question")
	}
	prompt := buildAnswerPrompt(title, context, question)
	return c.chat(ctx, prompt)
}

func (c *openAIClient) SuggestNotes(ctx context.Context, title, abstract string, contributions []string, content string) ([]SuggestedNote, error) {
	context := buildSuggestionContext(abstract, contributions, content, maxSuggestionChars)
	if context == "" {
		return nil, fmt.Errorf("paper text empty; cannot suggest notes")
	}
	prompt := buildSuggestionPrompt(title, context)
	raw, err := c.chat(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return parseSuggestedNotes(raw)
}

func (c *openAIClient) chat(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a concise research assistant."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s/chat/completions", c.base)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("openai API error: %s (%s)", resp.Status, string(body))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai API returned no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
