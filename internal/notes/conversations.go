package notes

import "time"

// ConversationSnapshot captures a point-in-time view of a paper session.
type ConversationSnapshot struct {
	EntryType       string                 `json:"entryType"`
	PaperID         string                 `json:"paperId"`
	PaperTitle      string                 `json:"paperTitle"`
	CapturedAt      time.Time              `json:"capturedAt"`
	Messages        []ConversationMessage  `json:"messages,omitempty"`
	Notes           []SnapshotNote         `json:"notes,omitempty"`
	Brief           *BriefSnapshot         `json:"brief,omitempty"`
	SectionMetadata []BriefSectionMetadata `json:"sectionMetadata,omitempty"`
	LLM             *LLMMetadata           `json:"llm,omitempty"`
}

// SnapshotUpdate appends new messages or notes to an existing snapshot.
type SnapshotUpdate struct {
	Messages []ConversationMessage `json:"messages,omitempty"`
	Notes    []SnapshotNote        `json:"notes,omitempty"`
}

// ConversationMessage records one transcript entry or user message.
type ConversationMessage struct {
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// SnapshotNote stores a note captured during a conversation.
type SnapshotNote struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

// BriefSnapshot stores the generated brief content at snapshot time.
type BriefSnapshot struct {
	Summary   []string `json:"summary,omitempty"`
	Technical []string `json:"technical,omitempty"`
	DeepDive  []string `json:"deepDive,omitempty"`
}

// BriefSectionMetadata captures per-section LLM status details.
type BriefSectionMetadata struct {
	Kind       string `json:"kind"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

// LLMMetadata captures the LLM provider details used for the snapshot.
type LLMMetadata struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}
