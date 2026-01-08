package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/csheth/browse/internal/tuitest"
)

func TestPaperScoutInitialHelpSnapshot(t *testing.T) {
	t.Parallel()

	cmdDir := moduleDir(t)
	fixture := filepath.Join(cmdDir, "testdata", "zettel_fixture.json")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}

	binary := buildBinary(t, cmdDir)
	ctx := context.Background()
	rec, err := tuitest.Run(ctx, tuitest.Config{
		Command: []string{binary, "-no-alt-screen", "-zettel", fixture},
		Dir:     cmdDir,
		Width:   100,
		Height:  32,
		Steps: []tuitest.Step{
			{Delay: 2500 * time.Millisecond},
			{Input: tuitest.KeyCtrlC},
		},
		Timeout:        5 * time.Second,
		AllowInterrupt: true,
	})
	if err != nil {
		t.Fatalf("run CLI: %v", err)
	}

	frame, ok := lastFrameMatching(rec, 32, []string{"PaperScout", "Conversation Stream", "Paste an arXiv"})
	if !ok {
		if _, has := rec.FinalFrame(); !has {
			t.Fatalf("no frames captured")
		}
		t.Fatalf("no UI frame captured")
	}

	snapshotPath := filepath.Join(cmdDir, "testdata", "snapshots", "initial_help.txt")
	assertSnapshot(t, snapshotPath, frame.Plain)
}

func moduleDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Dir(file)
}

func buildBinary(t *testing.T, cmdDir string) string {
	t.Helper()
	tmp := t.TempDir()
	name := "paperscout-integration"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath := filepath.Join(tmp, name)
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = cmdDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return binPath
}

func assertSnapshot(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("PAPERSCOUT_UPDATE_SNAPSHOTS") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create snapshot dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		t.Skipf("snapshot updated: %s", path)
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	wantStr := string(want)
	if wantStr != got+"\n" && wantStr != got {
		t.Fatalf("snapshot mismatch\n---- want ----\n%s\n---- got ----\n%s", wantStr, got)
	}
}

func lastFrameMatching(rec *tuitest.Recording, height int, tokens []string) (tuitest.Frame, bool) {
	if rec == nil {
		return tuitest.Frame{}, false
	}
	for i := len(rec.Frames) - 1; i >= 0; i-- {
		plain := rec.Frames[i].Plain
		for _, token := range tokens {
			if strings.Contains(plain, token) {
				return cropFrame(rec.Frames[i], height), true
			}
		}
	}
	return tuitest.Frame{}, false
}

func cropFrame(frame tuitest.Frame, height int) tuitest.Frame {
	frame.Plain = cropPlain(frame.Plain, height)
	return frame
}

func cropPlain(plain string, height int) string {
	trimmed := strings.TrimRight(plain, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= height {
		return trimmed
	}
	return strings.Join(lines[len(lines)-height:], "\n")
}
