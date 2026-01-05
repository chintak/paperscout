package notes

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	entryTypeConversation = "conversation"
	entryTypeNote         = "note"
)

type entryHeader struct {
	EntryType string `json:"entryType"`
}

// Save appends notes to the knowledge base file, creating it if necessary.
func Save(path string, newNotes []Note) error {
	if len(newNotes) == 0 {
		return nil
	}
	entries := make([]json.RawMessage, 0, len(newNotes))
	for _, note := range newNotes {
		raw, err := json.Marshal(note)
		if err != nil {
			return err
		}
		entries = append(entries, raw)
	}
	return appendEntries(path, entries)
}

// SaveConversationSnapshots appends conversation snapshots to the knowledge base file.
func SaveConversationSnapshots(path string, snapshots []ConversationSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	entries := make([]json.RawMessage, 0, len(snapshots))
	for _, snapshot := range snapshots {
		snapshot.EntryType = entryTypeConversation
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		entries = append(entries, raw)
	}
	return appendEntries(path, entries)
}

// AppendConversationSnapshot appends messages or notes to a per-paper snapshot.
func AppendConversationSnapshot(path, paperID, paperTitle string, update SnapshotUpdate) error {
	if path == "" || paperID == "" {
		return nil
	}
	if len(update.Messages) == 0 && len(update.Notes) == 0 && update.Brief == nil && len(update.SectionMetadata) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entries, err := loadEntries(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries = nil
	}
	updated := false
	capturedAt := time.Now()
	for i, raw := range entries {
		entryType, err := detectEntryType(raw)
		if err != nil {
			return err
		}
		if entryType != entryTypeConversation {
			continue
		}
		var snapshot ConversationSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		if snapshot.PaperID != paperID {
			continue
		}
		snapshot.EntryType = entryTypeConversation
		if snapshot.PaperTitle == "" {
			snapshot.PaperTitle = paperTitle
		}
		if snapshot.CapturedAt.IsZero() {
			snapshot.CapturedAt = capturedAt
		}
		snapshot.Messages = append(snapshot.Messages, update.Messages...)
		snapshot.Notes = append(snapshot.Notes, update.Notes...)
		if update.Brief != nil {
			if snapshot.Brief == nil {
				snapshot.Brief = &BriefSnapshot{}
			}
			if update.Brief.Summary != nil {
				snapshot.Brief.Summary = append([]string(nil), update.Brief.Summary...)
			}
			if update.Brief.Technical != nil {
				snapshot.Brief.Technical = append([]string(nil), update.Brief.Technical...)
			}
			if update.Brief.DeepDive != nil {
				snapshot.Brief.DeepDive = append([]string(nil), update.Brief.DeepDive...)
			}
		}
		if len(update.SectionMetadata) > 0 {
			snapshot.SectionMetadata = mergeSectionMetadata(snapshot.SectionMetadata, update.SectionMetadata)
		}
		raw, err = json.Marshal(snapshot)
		if err != nil {
			return err
		}
		entries[i] = raw
		updated = true
		break
	}
	if !updated {
		brief := copyBriefSnapshot(update.Brief)
		snapshot := ConversationSnapshot{
			EntryType:  entryTypeConversation,
			PaperID:    paperID,
			PaperTitle: paperTitle,
			CapturedAt: capturedAt,
			Messages:   update.Messages,
			Notes:      update.Notes,
			Brief:      brief,
			SectionMetadata: append([]BriefSectionMetadata(nil),
				update.SectionMetadata...),
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		entries = append(entries, raw)
	}
	return writeEntries(path, entries)
}

// Load returns all stored notes from the knowledge base.
func Load(path string) ([]Note, error) {
	entries, err := loadEntries(path)
	if err != nil {
		return nil, err
	}

	notes := make([]Note, 0, len(entries))
	for _, raw := range entries {
		entryType, err := detectEntryType(raw)
		if err != nil {
			return nil, err
		}
		if entryType == entryTypeConversation {
			continue
		}
		if entryType != entryTypeNote {
			continue
		}
		var note Note
		if err := json.Unmarshal(raw, &note); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, nil
}

// LoadConversationSnapshots returns all stored conversation snapshots from the knowledge base.
func LoadConversationSnapshots(path string) ([]ConversationSnapshot, error) {
	entries, err := loadEntries(path)
	if err != nil {
		return nil, err
	}

	snapshots := make([]ConversationSnapshot, 0)
	for _, raw := range entries {
		entryType, err := detectEntryType(raw)
		if err != nil {
			return nil, err
		}
		if entryType != entryTypeConversation {
			continue
		}
		var snapshot ConversationSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func appendEntries(path string, newEntries []json.RawMessage) error {
	if len(newEntries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entries, err := loadEntries(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries = nil
	}
	entries = append(entries, newEntries...)
	return writeEntries(path, entries)
}

func writeEntries(path string, entries []json.RawMessage) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadEntries(path string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func detectEntryType(raw json.RawMessage) (string, error) {
	var header entryHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return "", err
	}
	if header.EntryType == "" {
		return entryTypeNote, nil
	}
	return header.EntryType, nil
}

func mergeSectionMetadata(existing, updates []BriefSectionMetadata) []BriefSectionMetadata {
	if len(updates) == 0 {
		return existing
	}
	result := append([]BriefSectionMetadata(nil), existing...)
	for _, update := range updates {
		if update.Kind == "" {
			continue
		}
		replaced := false
		for i := range result {
			if result[i].Kind == update.Kind {
				result[i] = update
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, update)
		}
	}
	return result
}

func copyBriefSnapshot(source *BriefSnapshot) *BriefSnapshot {
	if source == nil {
		return nil
	}
	copy := BriefSnapshot{
		Summary:   append([]string(nil), source.Summary...),
		Technical: append([]string(nil), source.Technical...),
		DeepDive:  append([]string(nil), source.DeepDive...),
	}
	return &copy
}
