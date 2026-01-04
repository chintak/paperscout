package tuitest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

const (
	defaultWidth   = 120
	defaultHeight  = 32
	defaultTimeout = 5 * time.Second
)

// Step represents a scripted user interaction that the harness will replay
// against the pseudo terminal. A delay of zero means the input is written
// immediately.
type Step struct {
	Delay time.Duration
	Input []byte
}

// Config configures how the harness spawns and drives the CLI program.
type Config struct {
	Command          []string
	Dir              string
	Env              []string
	Width            int
	Height           int
	Steps            []Step
	Timeout          time.Duration
	AllowedExitCodes []int
	AllowInterrupt   bool
}

// Recording contains the raw terminal stream plus parsed frames.
type Recording struct {
	Raw      []byte
	Frames   []Frame
	Duration time.Duration
}

// Run executes the configured command inside a PTY, replays the scripted
// inputs, and captures every byte written to the terminal.
func Run(ctx context.Context, cfg Config) (*Recording, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("tuitest: command is required")
	}
	width := cfg.Width
	if width <= 0 {
		width = defaultWidth
	}
	height := cfg.Height
	if height <= 0 {
		height = defaultHeight
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Dir
	cmd.Env = buildEnv(cfg.Env)

	allowedCodes := map[int]struct{}{0: {}}
	for _, code := range cfg.AllowedExitCodes {
		allowedCodes[code] = struct{}{}
	}

	winsize := &pty.Winsize{Rows: uint16(height), Cols: uint16(width)}
	ptmx, err := pty.StartWithSize(cmd, winsize)
	if err != nil {
		return nil, fmt.Errorf("tuitest: start program: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	var output bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		responder := newTerminalResponder(ptmx)
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				responder.Process(chunk)
				_, _ = output.Write(chunk)
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) || errors.Is(readErr, os.ErrClosed) {
					return
				}
				return
			}
		}
	}()

	start := time.Now()
	for _, step := range cfg.Steps {
		if step.Delay > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("tuitest: context cancelled before script finished: %w", ctx.Err())
			case <-time.After(step.Delay):
			}
		}
		if len(step.Input) > 0 {
			if _, err := ptmx.Write(step.Input); err != nil {
				return nil, fmt.Errorf("tuitest: write input: %w", err)
			}
		}
	}

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	select {
	case err := <-waitErr:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if _, ok := allowedCodes[exitErr.ExitCode()]; ok {
					break
				}
			}
			if cfg.AllowInterrupt && strings.Contains(err.Error(), "signal: interrupt") {
				break
			}
			return nil, fmt.Errorf("tuitest: program exited with error: %w", err)
		}
	case <-ctx.Done():
		return nil, fmt.Errorf("tuitest: timeout waiting for program exit: %w", ctx.Err())
	}

	// Closing the PTY lets the reader goroutine finish draining.
	_ = ptmx.Close()
	<-copyDone

	raw := output.Bytes()
	frames := parseFrames(raw)
	duration := time.Since(start)
	return &Recording{Raw: raw, Frames: frames, Duration: duration}, nil
}

func buildEnv(extra []string) []string {
	env := os.Environ()
	env = append(env, extra...)
	termSet := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "TERM=") {
			termSet = true
			break
		}
	}
	if !termSet {
		env = append(env, "TERM=xterm-256color")
	}
	return env
}

var (
	// KeyEnter sends a carriage return to the PTY.
	KeyEnter = []byte{'\r'}
	// KeyCtrlC requests the program to terminate.
	KeyCtrlC = []byte{3}
	// KeyEsc exits transient overlays inside the TUI.
	KeyEsc = []byte{27}
)
