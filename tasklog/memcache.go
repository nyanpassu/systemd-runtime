package tasklog

import (
	"io"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/projecteru2/systemd-runtime/utils/collections/queue"
)

const MEM_LIMIT = 1024 //nolint:revive

type memCachedLog struct {
	sync.Mutex
	name   string
	fifo   io.WriteCloser
	buffer queue.BytesQueue
	closed bool
}

func newMemCachedLog(fifo SelfRestoreFifo) io.WriteCloser {
	log := &memCachedLog{
		fifo:   fifo,
		buffer: queue.NewBytesQueue(),
		name:   fifo.Name(),
	}
	go log.handleFifoSignal(fifo.Signal())
	return log
}

func (log *memCachedLog) Write(b []byte) (int, error) {
	log.Lock()
	defer log.Unlock()

	if log.closed {
		return 0, ErrFifoClosed
	}

	if log.buffer.Empty() {
		return log.lockFreeWriteFifo(b)
	}

	logrus.Debugf("buffer is not empty, append log to buffer, %s", log.name)
	log.unlockedPushBuffer(b)
	return len(b), nil
}

func (log *memCachedLog) Close() error {
	log.Lock()
	defer log.Unlock()

	logrus.Debugf("closing memcache, %s", log.name)
	log.closed = true
	if log.buffer.Empty() {
		_ = log.fifo.Close()
		return nil
	}
	return nil
}

func (log *memCachedLog) lockFreeWriteFifo(b []byte) (int, error) {
	cnt, err := log.fifo.Write(b)
	if err == nil {
		logrus.Debugf("write log to fifo success, %s", log.name)
		return cnt, nil
	}

	if err == ErrFifoNotOpened {
		logrus.Debugf("fifo log not opened, write to buffer, %s", log.name)
		log.unlockedPushBuffer(b)
		return len(b), nil
	}

	if err == ErrFifoClosed {
		logrus.Errorf("fifo is closed, close memcache log, %s", log.name)
		_ = log.Close()
		return cnt, err
	}

	// we have encountered some error, but all data has been written
	// the fifo will handle the error it self, but we give a warning here
	size := len(b)
	if cnt == size {
		logrus.Debugf("write log to fifo success despite error countered, %s, %v", log.name, err)
		return 0, nil
	}

	logrus.Debugf("write log to fifo partial success, write remain to buffer, error countered, %s, %v", log.name, err)
	log.unlockedPushBuffer(b[cnt:])
	return size, nil
}

func (log *memCachedLog) handleFifoSignal(ch <-chan struct{}) {
	for range ch {
		log.flushBufferToFifo()
		func() {
			log.Lock()
			defer log.Unlock()

			if log.closed && log.buffer.Empty() {
				log.fifo.Close()
				for range ch {
				}
				return
			}
		}()
	}

	logrus.Debugf("fifo close signal, close memcache log, %s", log.name)
	_ = log.Close()
}

func (log *memCachedLog) flushBufferToFifo() {
	log.Lock()
	defer log.Unlock()

	if log.buffer.Empty() {
		return
	}

	for !log.buffer.Empty() {
		buff := log.buffer.PeekHead()
		cnt, err := log.lockFreeWriteFifo(*buff)
		if err == nil {
			_ = log.buffer.PopHead()
			continue
		}

		if cnt == len(*buff) {
			_ = log.buffer.PopHead()
			return
		}

		*buff = (*buff)[cnt:]
		return
	}
}

func (log *memCachedLog) unlockedPushBuffer(bytes []byte) {
	if log.buffer.Size() == MEM_LIMIT {
		log.buffer.PopHead()
	}
	log.buffer.PushTail(bytes)
}
