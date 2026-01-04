package tui

import "testing"

func TestPageLayoutUpdate(t *testing.T) {
	cases := []struct {
		name             string
		width            int
		height           int
		viewportWidth    int
		viewportHeight   int
		transcriptHeight int
		composerHeight   int
	}{
		{name: "narrow", width: 80, height: 24, viewportWidth: 76, viewportHeight: 6, transcriptHeight: 6, composerHeight: 4},
		{name: "wide", width: 200, height: 40, viewportWidth: 196, viewportHeight: 19, transcriptHeight: 9, composerHeight: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newPageLayout()
			layout.Update(tc.width, tc.height)
			if layout.viewportWidth != tc.viewportWidth {
				t.Fatalf("viewport width mismatch: got %d want %d", layout.viewportWidth, tc.viewportWidth)
			}
			if layout.viewportHeight != tc.viewportHeight {
				t.Fatalf("viewport height mismatch: got %d want %d", layout.viewportHeight, tc.viewportHeight)
			}
			if layout.transcriptHeight != tc.transcriptHeight {
				t.Fatalf("transcript height mismatch: got %d want %d", layout.transcriptHeight, tc.transcriptHeight)
			}
			if layout.composerHeight != tc.composerHeight {
				t.Fatalf("composer height mismatch: got %d want %d", layout.composerHeight, tc.composerHeight)
			}
		})
	}
}
