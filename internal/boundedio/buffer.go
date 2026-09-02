// Package boundedio provides bounded in-memory writers.
package boundedio

import "bytes"

// Buffer retains at most limit bytes while reporting successful writes to its
// producer. Truncated reports whether additional bytes were discarded.
type Buffer struct {
	data      bytes.Buffer
	limit     int
	truncated bool
}

// NewBuffer creates a buffer that retains at most limit bytes.
func NewBuffer(limit int) *Buffer {
	return &Buffer{limit: max(limit, 0)}
}

// Write retains the bounded prefix and accepts the complete input.
func (b *Buffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := b.limit - b.data.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.data.Write(value)
	}
	if originalLength > remaining {
		b.truncated = true
	}
	return originalLength, nil
}

func (b *Buffer) String() string { return b.data.String() }

// Truncated reports whether bytes were discarded after the limit was reached.
func (b *Buffer) Truncated() bool { return b.truncated }
