package tasklog

import (
	"io"
	"sync"

	"github.com/sirupsen/logrus"
)

const LIMIT = 1024

type cachedLog struct {
	sync.Mutex
	buff   []byte
	buffer [][]byte
}

func (log *cachedLog) HasCachedContent() bool {
	log.Lock()
	defer log.Unlock()

	return log.buff != nil || len(log.buffer) > 0
}

func (log *cachedLog) Pipe(w io.Writer) error {
	ch := make(chan []byte)
	next := make(chan struct{})
	closed := make(chan struct{})

	go func() {
		defer close(ch)
		defer close(next)
		defer close(closed)

		for {
			buffer := func() []byte {
				log.Lock()
				defer log.Unlock()
				if log.buff != nil {
					return log.buff
				}

				if len(log.buffer) == 0 {
					return nil
				}
				buffer := log.buffer[0]
				log.buff = buffer
				log.buffer = log.buffer[1:]

				return buffer
			}()

			if buffer == nil {
				return
			}

			ch <- buffer
			select {
			case <-next:
			case <-closed:
				return
			}
		}
	}()

	for buffer := range ch {
		cnt, err := w.Write(buffer)
		if err == nil {
			func() {
				log.Lock()
				defer log.Unlock()

				log.buff = nil
			}()
			next <- struct{}{}
			continue
		}

		buffer = buffer[cnt:]
		func() {
			log.Lock()
			defer log.Unlock()

			if len(buffer) > 0 {
				log.buff = buffer
				return
			}
			log.buff = nil
		}()
		closed <- struct{}{}
		return err
	}

	logrus.Debug("cached content has all been piped")
	return nil
}

func (log *cachedLog) Write(b []byte) (int, error) {
	log.Lock()
	defer log.Unlock()

	size := len(log.buffer)
	if size >= LIMIT {
		log.buffer = append(log.buffer[size-LIMIT+1:], b)
	} else {
		log.buffer = append(log.buffer, b)
	}

	return len(b), nil
}

func (log *cachedLog) Close() error {
	return nil
}

func (log *cachedLog) Reset() error {
	return nil
}
