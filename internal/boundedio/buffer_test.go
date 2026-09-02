package boundedio

import "testing"

func TestBufferCapsRetainedOutput(t *testing.T) {
	t.Parallel()
	buffer := NewBuffer(8)
	value := []byte("0123456789abcdef")
	written, err := buffer.Write(value)
	if err != nil || written != len(value) {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if buffer.String() != "01234567" || !buffer.Truncated() {
		t.Fatalf("bounded buffer = %q, truncated=%v", buffer.String(), buffer.Truncated())
	}
}
