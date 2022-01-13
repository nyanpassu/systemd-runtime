package tasklog

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockedFifoHandle struct {
	ch <-chan struct {
		io.WriteCloser
		error
	}
}

func (h *mockedFifoHandle) OpenFifo() (io.WriteCloser, error) {
	fifo := <-h.ch
	return fifo.WriteCloser, fifo.error
}

func (h *mockedFifoHandle) Close() error {
	return nil
}

type mockedWriteCloser struct {
	ch   chan []byte
	buff [][]byte
}

func (fifo *mockedWriteCloser) Write(b []byte) (int, error) {
	if fifo.ch != nil {
		fifo.ch <- b
	}

	fifo.buff = append(fifo.buff, b)
	return len(b), nil
}

func (fifo *mockedWriteCloser) Close() error {
	if fifo.ch != nil {
		close(fifo.ch)
	}
	return nil
}

func TestFifo(t *testing.T) {
	_, err := newFifo("test", func(fn string) (FifoHandle, error) {
		return nil, errors.New("create fifo error")
	})
	assert.Error(t, err, "fifo create unsuccess as expected")

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
	cnt, err := fifo.Write(b)
	assert.Equal(t, ErrFifoNotOpened, err)
	assert.Equal(t, 0, cnt)

	file := mockedWriteCloser{}

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

	cnt, err = fifo.Write(b)
	assert.NoError(t, err)
	assert.Equal(t, len(b), cnt)
}
