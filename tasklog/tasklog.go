package tasklog

import (
	"io"
)

// NewLogger .
func NewLogger(fn string) (io.WriteCloser, error) {
	fifo, err := newFifo(fn, getOrCreateHandle)
	if err != nil {
		return nil, err
	}

	return newMemCachedLog(fifo), nil
}
