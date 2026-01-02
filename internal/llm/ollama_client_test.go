package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaClientSummarize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var payload struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if payload.Model != "qwen3-vl:8b" {
			t.Fatalf("expected model qwen3-vl:8b, got %s", payload.Model)
		}
		if !strings.Contains(payload.Prompt, "Paper title: Cool Paper") {
			t.Fatalf("prompt missing title: %s", payload.Prompt)
		}
		if payload.Stream {
			t.Fatal("expected streaming to be disabled")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":"Bullet 1","done":true}`))
	}))
	defer server.Close()

	client := &ollamaClient{
		host:   server.URL,
		model:  "qwen3-vl:8b",
		client: server.Client(),
	}

	result, err := client.Summarize(context.Background(), "Cool Paper", "This is the PDF content.")
	if err != nil {
		t.Fatalf("summarize failed: %v", err)
	}
	if result != "Bullet 1" {
		t.Fatalf("unexpected summarize result: %s", result)
	}
}

func TestOllamaClientAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if payload.Model != "qwen3-vl:8b" {
			t.Fatalf("expected model qwen3-vl:8b, got %s", payload.Model)
		}
		if !strings.Contains(payload.Prompt, "Question: What is the method?") {
			t.Fatalf("prompt missing question: %s", payload.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":"The paper uses contrastive learning.","done":true}`))
	}))
	defer server.Close()

	client := &ollamaClient{
		host:   server.URL,
		model:  "qwen3-vl:8b",
		client: server.Client(),
	}

	answer, err := client.Answer(context.Background(), "Cool Paper", "What is the method?", "The method leverages contrastive learning across modalities.")
	if err != nil {
		t.Fatalf("answer failed: %v", err)
	}
	if answer != "The paper uses contrastive learning." {
		t.Fatalf("unexpected answer: %s", answer)
	}
}

func TestOllamaClientSuggestNotes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if !strings.Contains(payload.Prompt, "How to Read a Paper") {
			t.Fatalf("prompt missing reference text: %s", payload.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":"{\"notes\":[{\"title\":\"Problem\",\"body\":\"Body text\",\"reason\":\"First pass\",\"kind\":\"problem\"}]}","done":true}`))
	}))
	defer server.Close()

	client := &ollamaClient{
		host:   server.URL,
		model:  "qwen3-vl:8b",
		client: server.Client(),
	}

	notes, err := client.SuggestNotes(context.Background(), "Cool Paper", "abstract", []string{"foo"}, "body")
	if err != nil {
		t.Fatalf("suggest failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].Title != "Problem" {
		t.Fatalf("unexpected title: %s", notes[0].Title)
	}
}
