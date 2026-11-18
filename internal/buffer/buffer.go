package buffer

import (
	"strings"
	"sync"
	"time"
)

type MessageBuffer struct {
	mu        sync.Mutex
	chunks    []string
	timer     *time.Timer
	wait      time.Duration
	threshold int
	onFlush   func(fullText string)
}

func New(threshold int, waitSeconds float64, onFlush func(string)) *MessageBuffer {
	if threshold <= 0 {
		threshold = 400
	}
	if waitSeconds <= 0 {
		waitSeconds = 4
	}
	return &MessageBuffer{
		threshold: threshold,
		wait:      time.Duration(waitSeconds * float64(time.Second)),
		onFlush:   onFlush,
	}
}

// Push agrega un fragmento. Si es corto y no había buffer previo, devuelve (text, true) inmediato.
func (b *MessageBuffer) Push(text string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.chunks) == 0 && len([]rune(text)) < b.threshold {
		return text, true
	}

	b.chunks = append(b.chunks, text)
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(b.wait, b.flush)
	return "", false
}

func (b *MessageBuffer) flush() {
	b.mu.Lock()
	if len(b.chunks) == 0 {
		b.mu.Unlock()
		return
	}
	joined := strings.Join(b.chunks, "\n")
	b.chunks = nil
	b.timer = nil
	cb := b.onFlush
	b.mu.Unlock()

	if cb != nil {
		cb(joined)
	}
}
