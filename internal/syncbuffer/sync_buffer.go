package syncbuffer

import (
	"bytes"
	"sync"
)

type SyncBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buf.String()
}
