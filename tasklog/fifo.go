//go:build !windows
// +build !windows

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package tasklog

import (
	"io"
	"sync"

	"github.com/sirupsen/logrus"
)

type SelfRestoreFifo interface {
	io.WriteCloser
	Name() string
	Signal() <-chan struct{}
}

func newFifo(fn string, makeFifoHandle func(string) (FifoHandle, error)) (SelfRestoreFifo, error) {
	handle, err := makeFifoHandle(fn)
	if err != nil {
		return nil, err
	}
	holder := &fifoHolder{
		fn:             fn,
		makeFifoHandle: makeFifoHandle,
	}
	go holder.syncOpenFifo(handle)

	return holder, nil
}

type fifoHolder struct {
	sync.Mutex
	fn             string
	closed         bool
	signalChs      []chan struct{}
	fifo           io.WriteCloser
	handle         FifoHandle
	makeFifoHandle func(string) (FifoHandle, error)
}

func (holder *fifoHolder) Signal() <-chan struct{} {
	holder.Lock()
	defer holder.Unlock()

	ch := make(chan struct{})
	holder.signalChs = append(holder.signalChs, ch)

	if holder.fifo != nil {
		go func() {
			ch <- struct{}{}
		}()
	}

	return ch
}

func (holder *fifoHolder) Name() string {
	return holder.fn
}

func (holder *fifoHolder) Write(b []byte) (int, error) {
	holder.Lock()
	defer holder.Unlock()

	if holder.closed {
		return 0, ErrFifoClosed
	}

	if holder.fifo == nil {
		return 0, ErrFifoNotOpened
	}

	cnt, err := holder.fifo.Write(b)
	if err != nil {
		holder.unlockedReset()
		return cnt, err
	}

	return cnt, nil
}

func (holder *fifoHolder) unlockedReset() {
	fifo := holder.fifo
	handle := holder.handle
	holder.fifo = nil
	holder.handle = nil

	go func() {
		if err := fifo.Close(); err != nil {
			logrus.Warnf("reset fifo holder, close fifo error, %s, cause = %v", holder.fn, err)
		}

		if err := handle.Close(); err != nil {
			logrus.Warnf("reset fifo holder, close handle error, %s, cause = %v", holder.fn, err)
		}

		handle, err := holder.makeFifoHandle(holder.fn)
		if err != nil {
			logrus.Errorf("reset fifo holder, get handle error, %s, cause = %v", holder.fn, err)
			holder.Close()
			return
		}
		holder.syncOpenFifo(handle)
	}()
}

func (holder *fifoHolder) syncOpenFifo(handle FifoHandle) {
	fifo, err := handle.OpenFifo()
	if err != nil {
		logrus.Errorf("open fifo error, %s, cause = %v", holder.fn, err)
		_ = handle.Close()
		_ = holder.Close()
		return
	}
	holder.Lock()
	defer holder.Unlock()

	if holder.closed {
		logrus.Warnf("fifo holder is closed, closed opened fifo, %s", holder.fn)
		_ = fifo.Close()
		_ = handle.Close()
		return
	}

	holder.handle = handle
	holder.fifo = fifo
	for _, ch := range holder.signalChs {
		ch <- struct{}{}
	}
}

func (holder *fifoHolder) Close() error {
	holder.Lock()
	defer holder.Unlock()

	return holder.unlockedClose()
}

func (holder *fifoHolder) unlockedClose() error {
	if holder.closed {
		return nil
	}
	logrus.Debugf("closing fifoholder, %s", holder.fn)

	holder.closed = true

	fifo := holder.fifo
	holder.fifo = nil

	for _, ch := range holder.signalChs {
		close(ch)
	}

	if fifo != nil {
		go func() {
			// we returned nil from close, so we will print error to stderr
			if err := fifo.Close(); err != nil {
				logrus.Errorf("close fifo error, %s, cause = %v", holder.fn, err)
			}
			logrus.Debugf("close fifo success, %s", holder.fn)
		}()
	}

	return nil
}
