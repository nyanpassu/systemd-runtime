package tasklog

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemCache(t *testing.T) {
	ch := make(chan struct {
		io.WriteCloser
		error
	})

	fifo, err := newFifo("test", func(fn string) (FifoHandle, error) {
		return &mockedFifoHandle{ch: ch}, nil
	})
	assert.NoError(t, err, "fifo create successful")
	// fifo.Signal()

	b := []byte{0, 1, 2, 3}

	memCache := newMemCachedLog(fifo)

	for i := 0; i < 1100; i++ {
		cnt, err := memCache.Write(b)
		assert.NoError(t, err, "write success")
		assert.Equal(t, len(b), cnt)
	}

	assert.Equal(t, MEM_LIMIT, memCache.(*memCachedLog).buffer.Size())

	chFifo := make(chan []byte)
	file := mockedWriteCloser{
		ch: chFifo,
	}

	go func() {
		ch <- struct {
			io.WriteCloser
			error
		}{
			WriteCloser: &file,
			error:       nil,
		}
		close(ch)
	}()
	sig := fifo.Signal()
	<-sig

	<-chFifo
	go func() {
		for range chFifo {

		}
	}()

	memCache.Close()

	assert.Equal(t, MEM_LIMIT, len(file.buff))
}
