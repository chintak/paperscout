package tuitest

import (
	"bytes"
	"io"
)

type terminalResponder struct {
	w   io.Writer
	buf []byte
}

func newTerminalResponder(w io.Writer) *terminalResponder {
	return &terminalResponder{w: w, buf: make([]byte, 0, 128)}
}

func (tr *terminalResponder) Process(chunk []byte) {
	tr.buf = append(tr.buf, chunk...)
	tr.scan()
	// Keep a small tail so we can detect sequences that span reads.
	if len(tr.buf) > 256 {
		tr.buf = tr.buf[len(tr.buf)-64:]
	}
}

func (tr *terminalResponder) scan() {
	for {
		switched := false
		if tr.consume([]byte("\x1b[6n"), []byte("\x1b[1;1R")) {
			switched = true
		}
		if tr.consume([]byte("\x1b]10;?\x07"), []byte("\x1b]10;rgb:cccc/cccc/cccc\x07")) {
			switched = true
		}
		if tr.consume([]byte("\x1b]10;?\x1b\\"), []byte("\x1b]10;rgb:cccc/cccc/cccc\x1b\\")) {
			switched = true
		}
		if tr.consume([]byte("\x1b]11;?\x07"), []byte("\x1b]11;rgb:0000/0000/0000\x07")) {
			switched = true
		}
		if tr.consume([]byte("\x1b]11;?\x1b\\"), []byte("\x1b]11;rgb:0000/0000/0000\x1b\\")) {
			switched = true
		}
		if !switched {
			return
		}
	}
}

func (tr *terminalResponder) consume(pattern, response []byte) bool {
	idx := bytes.Index(tr.buf, pattern)
	if idx < 0 {
		return false
	}
	// Drop everything up to and including the detected pattern to keep the buffer small.
	tr.buf = tr.buf[idx+len(pattern):]
	_, _ = tr.w.Write(response)
	return true
}
