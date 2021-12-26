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
	"os"
	"sync"
	"syscall"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type OnResetComplete func(io.Writer, ResetCloser)
type ResetCloser interface {
	io.Closer
	Reset(OnResetComplete)
}

func newFifoHolder(fn string, s OnResetComplete) (holder *fifoHolder, err error) {
	if _, err := os.Stat(fn); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := syscall.Mkfifo(fn, 0); err != nil && !os.IsExist(err) {
			return nil, errors.Wrapf(err, "error creating fifo %v", fn)
		}
	}

	holder = &fifoHolder{
		fn:          fn,
		subscribers: []OnResetComplete{s},
	}

	go func() {
		fifo, err := openFifo(holder.fn)
		holder.set(fifo, err)
	}()

	return holder, nil
}

type fifoHolder struct {
	sync.Mutex
	fn          string
	closed      bool
	fifo        io.WriteCloser
	err         error
	subscribers []OnResetComplete
}

func (holder *fifoHolder) get() (io.WriteCloser, error) {
	holder.Lock()
	defer holder.Unlock()
	return holder.fifo, holder.err
}

func (holder *fifoHolder) set(wc io.WriteCloser, err error) {
	subscribers := func() []OnResetComplete {
		holder.Lock()
		defer holder.Unlock()
		holder.fifo = wc
		holder.err = err

		subscibers := holder.subscribers
		holder.subscribers = nil
		return subscibers
	}()
	if err == nil {
		for _, subscriber := range subscribers {
			subscriber(wc, holder)
		}
	}
}

func (holder *fifoHolder) holderClosed() bool {
	holder.Lock()
	defer holder.Unlock()

	return holder.closed
}

func (holder *fifoHolder) Reset(s OnResetComplete) {
	holder.Lock()
	defer holder.Unlock()

	holder.subscribers = append(holder.subscribers, s)

	fifo := holder.fifo
	holder.fifo = nil
	holder.err = nil

	go func() {
		var err error
		if err = fifo.Close(); err != nil {
			logrus.Errorf("close fifo error, cause: %v", err)
		}
		fifo, err = openFifo(holder.fn)
		if holder.holderClosed() {
			_ = fifo.Close()
			return
		}
		holder.set(fifo, err)
	}()
}

func (holder *fifoHolder) Close() error {
	holder.Lock()
	defer holder.Unlock()

	if holder.closed {
		return nil
	}

	holder.subscribers = nil
	holder.closed = true
	holder.err = ErrFifoClosed
	if holder.fifo != nil {
		_ = holder.fifo.Close()
	}

	return nil
}

func openFifo(fn string) (f *fifo, err error) {
	logrus.Debugf("open fifo for task log %s", fn)
	var (
		handle *handle
		path   string
	)
	handle, err = getHandle(fn)
	if err != nil {
		logrus.Errorf("get fifo handle for task log error, %s, cause = %v", fn, err)
		return nil, err
	}

	defer func() {
		if err != nil {
			logrus.Errorf("open fifo for task log error, %s, cause = %v", fn, err)
			_ = handle.Close()
		}
	}()
	path, err = handle.Path()
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, syscall.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("open fifo for task log success %s", fn)
	return &fifo{
		file:    file,
		handler: handle,
	}, nil
}

type fifo struct {
	once    sync.Once
	file    *os.File
	handler *handle
}

func (f *fifo) Write(b []byte) (int, error) {
	return f.file.Write(b)
}

func (f *fifo) Close() error {
	_ = f.handler.Close()
	f.once.Do(func() {
		_ = f.file.Close()
	})
	return nil
}
