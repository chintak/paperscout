package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
			{Delay: time.Second},
			{Input: []byte("?")},
			{Delay: time.Second},
			{Input: tuitest.KeyCtrlC},
		},
		Timeout:        5 * time.Second,
		AllowInterrupt: true,
	})
	if err != nil {
		t.Fatalf("run CLI: %v", err)
	}

	frame, ok := rec.FinalFrame()
	if !ok {
		t.Fatalf("no frames captured")
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
