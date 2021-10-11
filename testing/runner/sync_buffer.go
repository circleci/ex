package runner

import (
	"bytes"
	"sync"
)

type syncBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buf.String()
}
