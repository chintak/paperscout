package tuitest

import (
	"regexp"
	"strings"
)

// Frame represents a normalized terminal render.
type Frame struct {
	Index int
	ANSI  string
	Plain string
}

var (
	frameSeparator = regexp.MustCompile(`\x1b\[[0-9;]*J`)
	csiPattern     = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	oscPattern     = regexp.MustCompile(`\x1b\][^\x07]*(\x07|\x1b\\)`)
)

func parseFrames(raw []byte) []Frame {
	cleaned := strings.ReplaceAll(string(raw), "\r", "")
	segments := frameSeparator.Split(cleaned, -1)
	frames := make([]Frame, 0, len(segments))
	for _, segment := range segments {
		segment = strings.Trim(segment, "\x00")
		segment = strings.TrimPrefix(segment, "\x1b[H")
		if segment == "" {
			continue
		}
		stripped := stripANSI(segment)
		if strings.TrimSpace(stripped) == "" {
			continue
		}
		frames = append(frames, Frame{
			Index: len(frames),
			ANSI:  segment,
			Plain: normalizeLines(stripped),
		})
	}
	if len(frames) == 0 && len(cleaned) > 0 {
		frames = append(frames, Frame{Index: 0, ANSI: cleaned, Plain: normalizeLines(stripANSI(cleaned))})
	}
	return frames
}

// FinalFrame returns the last captured frame. The second return value is false
// when no frames were recorded.
func (r *Recording) FinalFrame() (Frame, bool) {
	if r == nil || len(r.Frames) == 0 {
		return Frame{}, false
	}
	return r.Frames[len(r.Frames)-1], true
}

func stripANSI(s string) string {
	s = oscPattern.ReplaceAllString(s, "")
	s = csiPattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\x0f", "")
	s = strings.ReplaceAll(s, "\x0e", "")
	return s
}

func normalizeLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
