package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOllamaClientSummarize(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
		if payload.Model != "ministral-3:latest" {
			t.Fatalf("expected model ministral-3:latest, got %s", payload.Model)
		}
		if !strings.Contains(payload.Prompt, "Paper title: Cool Paper") {
			t.Fatalf("prompt missing title: %s", payload.Prompt)
		}
		if payload.Stream {
			t.Fatal("expected streaming to be disabled")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"response":"Bullet 1","done":true}`)),
			Header:     make(http.Header),
		}, nil
	})

	client := &ollamaClient{
		host:   "http://example.com",
		model:  "ministral-3:latest",
		client: &http.Client{Transport: rt},
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
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if payload.Model != "ministral-3:latest" {
			t.Fatalf("expected model ministral-3:latest, got %s", payload.Model)
		}
		if !strings.Contains(payload.Prompt, "Question: What is the method?") {
			t.Fatalf("prompt missing question: %s", payload.Prompt)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"response":"The paper uses contrastive learning.","done":true}`)),
			Header:     make(http.Header),
		}, nil
	})

	client := &ollamaClient{
		host:   "http://example.com",
		model:  "ministral-3:latest",
		client: &http.Client{Transport: rt},
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
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"response":"{\"notes\":[{\"title\":\"Problem\",\"body\":\"Body text\",\"reason\":\"First pass\",\"kind\":\"problem\"}]}","done":true}`)),
			Header:     make(http.Header),
		}, nil
	})

	client := &ollamaClient{
		host:   "http://example.com",
		model:  "ministral-3:latest",
		client: &http.Client{Transport: rt},
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

func TestOllamaClientReadingBrief(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(payload.Prompt, "\"deepDive\"") {
			t.Fatalf("prompt missing schema: %s", payload.Prompt)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"response":"{\"summary\":[\"s1\"],\"technical\":[\"t1\"],\"deepDive\":[\"d1\"]}","done":true}`)),
			Header:     make(http.Header),
		}, nil
	})

	client := &ollamaClient{
		host:   "http://example.com",
		model:  "ministral-3:latest",
		client: &http.Client{Transport: rt},
	}
	brief, err := client.ReadingBrief(context.Background(), "Cool Paper", "body text")
	if err != nil {
		t.Fatalf("reading brief failed: %v", err)
	}
	if len(brief.Summary) != 1 || brief.Summary[0] != "s1" {
		t.Fatalf("unexpected summary: %#v", brief.Summary)
	}
}
