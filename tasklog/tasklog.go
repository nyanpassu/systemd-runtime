package tasklog

import (
	"io"

	"github.com/sirupsen/logrus"
)

// CachedLog .
type CachedLog interface {
	io.WriteCloser
	HasCachedContent() bool
	Pipe(io.Writer) error
	Reset() error
}

type taskLog struct {
	fifo      *fifoHolder
	cachedLog CachedLog
}

// NewLogger .
func NewLogger(fn string) (io.WriteCloser, error) {
	cachedLog := &cachedLog{}
	fifo, err := newFifoHolder(fn, func(resetW io.Writer, resetRC ResetCloser) {
		if cachedLog.HasCachedContent() {
			onNamedPipeReady(resetW, resetRC, cachedLog)
		}
	})
	if err != nil {
		return nil, err
	}
	return &taskLog{
		fifo:      fifo,
		cachedLog: cachedLog,
	}, nil
}

func (log *taskLog) Write(b []byte) (int, error) {
	if log.cachedLog.HasCachedContent() {
		logrus.Debug("has cached content, writing to cached content")
		return log.cachedLog.Write(b)
	}

	fifo, err := log.fifo.get()
	if err != nil {
		logrus.Errorf("fifo has closed, cause = %v", err)
		return 0, err
	}
	if fifo != nil {
		logrus.Debug("write log content to fifo")
		cnt, err := fifo.Write(b)
		if err == nil {
			return cnt, nil
		}
		if err != nil {
			logrus.Warnf("write fifo error, reset fifo, cause = %v", err)
			log.fifo.Reset(func(resetW io.Writer, resetRC ResetCloser) {
				onNamedPipeReady(resetW, resetRC, log.cachedLog)
			})
		}
		b = b[cnt:]
	}
	return log.cachedLog.Write(b)
}

func (log *taskLog) Close() error {
	_ = log.fifo.Close()
	_ = log.cachedLog.Close()
	return nil
}

func onNamedPipeReady(w io.Writer, resetCloser ResetCloser, cachedLog CachedLog) {
	logrus.Debug("named pipe is ready, prepare pipe cached content to fifo")
	go func() {
		for {
			err := cachedLog.Pipe(w)
			if err == nil {
				return
			}
			if err == ErrFifoClosed {
				logrus.Warnf("fifo is closed when pipe cached content to fifo error, will try reset fifo")
				resetCloser.Reset(func(resetW io.Writer, resetRC ResetCloser) {
					onNamedPipeReady(resetW, resetRC, cachedLog)
				})
				return
			}
			logrus.Warnf("pipe cached content to fifo error, cause = %v", err)
			if err := cachedLog.Reset(); err != nil {
				logrus.Errorf("reset cached log error, cause = %v", err)
				_ = resetCloser.Close()
				_ = cachedLog.Close()
				return
			}
		}
	}()
}
