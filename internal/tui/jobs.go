package tui

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type jobKind string

type jobStatus string

const (
	jobKindFetch    jobKind = "fetch"
	jobKindSummary  jobKind = "summary"
	jobKindSuggest  jobKind = "suggest"
	jobKindSave     jobKind = "save"
	jobKindQuestion jobKind = "question"
)

const (
	jobStatusRunning   jobStatus = "running"
	jobStatusSucceeded jobStatus = "succeeded"
	jobStatusFailed    jobStatus = "failed"
)

type jobSnapshot struct {
	ID          string
	Kind        jobKind
	Status      jobStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Err         string
	Duration    time.Duration
}

type jobSignalMsg struct {
	Snapshot jobSnapshot
}

type jobResultEnvelope struct {
	Snapshot jobSnapshot
	Payload  tea.Msg
}

type jobRunner func(context.Context) (tea.Msg, error)

type jobBus struct {
	counter int64
}

func newJobBus() *jobBus {
	return &jobBus{}
}

func (b *jobBus) nextID(kind jobKind) string {
	idx := atomic.AddInt64(&b.counter, 1)
	return fmt.Sprintf("%s-%d", kind, idx)
}

func (b *jobBus) Start(kind jobKind, runner jobRunner) tea.Cmd {
	id := b.nextID(kind)
	started := time.Now()
	startSnapshot := jobSnapshot{ID: id, Kind: kind, Status: jobStatusRunning, StartedAt: started}
	startCmd := func() tea.Msg {
		return jobSignalMsg{Snapshot: startSnapshot}
	}

	runCmd := func() tea.Msg {
		ctx := context.Background()
		payload, err := runner(ctx)
		snapshot := jobSnapshot{
			ID:          id,
			Kind:        kind,
			StartedAt:   started,
			CompletedAt: time.Now(),
		}
		if err != nil {
			snapshot.Status = jobStatusFailed
			snapshot.Err = err.Error()
		} else {
			snapshot.Status = jobStatusSucceeded
		}
		snapshot.Duration = snapshot.CompletedAt.Sub(started)
		log.Printf("[jobs] %s %s (duration=%s, err=%v)", kind, snapshot.Status, snapshot.Duration, err)
		return jobResultEnvelope{Snapshot: snapshot, Payload: payload}
	}

	return tea.Sequence(startCmd, runCmd)
}
