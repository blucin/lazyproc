package process

import "sync"

// RingBuffer is a fixed-size circular buffer that stores string lines.
// When full, new lines overwrite the oldest ones.
// All methods are safe for concurrent use.
type RingBuffer struct {
	mu       sync.RWMutex
	buf      []string
	size     int
	head     int // index where the next write will go
	count    int // number of valid entries currently stored
}

// NewRingBuffer creates a new RingBuffer with the given maximum line capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 10000
	}
	return &RingBuffer{
		buf:  make([]string, size),
		size: size,
	}
}

// Push appends a line to the buffer.
// If the buffer is full the oldest line is silently dropped.
func (r *RingBuffer) Push(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf[r.head] = line
	r.head = (r.head + 1) % r.size

	if r.count < r.size {
		r.count++
	}
}

// Lines returns a snapshot of all stored lines in insertion order (oldest first).
func (r *RingBuffer) Lines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 {
		return nil
	}

	out := make([]string, r.count)

	// When the buffer is not yet full, entries start at index 0.
	// When full, the oldest entry is at r.head (the slot about to be overwritten).
	var start int
	if r.count < r.size {
		start = 0
	} else {
		start = r.head
	}

	for i := 0; i < r.count; i++ {
		out[i] = r.buf[(start+i)%r.size]
	}

	return out
}

// Len returns the number of lines currently stored.
func (r *RingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Cap returns the maximum number of lines the buffer can hold.
func (r *RingBuffer) Cap() int {
	return r.size
}

// Clear removes all lines from the buffer.
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head = 0
	r.count = 0
	// Zero out the backing slice so old strings can be GC'd.
	for i := range r.buf {
		r.buf[i] = ""
	}
}
