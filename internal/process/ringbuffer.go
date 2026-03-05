package process

import (
	"bytes"
	"strings"

	rb "github.com/smallnest/ringbuffer"
)

// avgBytesPerLine for rough estimation purposes
// to determine suitable default capacity for the ring buffer.
const avgBytesPerLine = 512

// RingBuffer is a circular line buffer
// When the byte capacity is exhausted, oldest bytes are discarded to make room.
// All methods are safe for concurrent use.
type RingBuffer struct {
	buf     *rb.RingBuffer
	maxLine int
}

func NewRingBuffer(maxLines int) *RingBuffer {
	if maxLines <= 0 {
		maxLines = 10_000
	}
	return &RingBuffer{
		buf:     rb.New(maxLines * avgBytesPerLine).SetOverwrite(true),
		maxLine: maxLines,
	}
}

func (r *RingBuffer) Push(line string) {
	_, _ = r.buf.Write([]byte(line + "\n"))
}

func (r *RingBuffer) Lines() []string {
	raw := r.buf.Bytes(nil)
	if len(raw) == 0 {
		return nil
	}
	content := strings.TrimRight(string(raw), "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func (r *RingBuffer) Len() int {
	raw := r.buf.Bytes(nil)
	if len(raw) == 0 {
		return 0
	}
	return bytes.Count(raw, []byte("\n"))
}

func (r *RingBuffer) Cap() int {
	return r.maxLine
}

func (r *RingBuffer) Clear() {
	r.buf.Reset()
}
