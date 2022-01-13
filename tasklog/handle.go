//go:build linux
// +build linux

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
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type FifoHandle interface {
	io.Closer
	OpenFifo() (io.WriteCloser, error)
}

//nolint:revive
const O_PATH = 010000000

type handle struct {
	f         *os.File
	fd        uintptr
	dev       uint64
	ino       uint64
	closeOnce sync.Once
	name      string
}

func getOrCreateHandle(fn string) (FifoHandle, error) {
	if _, err := os.Stat(fn); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := syscall.Mkfifo(fn, 0); err != nil && !os.IsExist(err) {
			return nil, errors.Wrapf(err, "error creating fifo %v", fn)
		}
	}

	f, err := os.OpenFile(fn, O_PATH, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %v with O_PATH", fn)
	}

	var (
		stat syscall.Stat_t
		fd   = f.Fd()
	)
	if err := syscall.Fstat(int(fd), &stat); err != nil {
		_ = f.Close()
		return nil, errors.Wrapf(err, "failed to stat handle %v", fd)
	}

	if stat.Mode&syscall.S_IFIFO != syscall.S_IFIFO {
		_ = f.Close()
		return nil, errors.Errorf("file %s is not fifo", fn)
	}

	h := &handle{
		f:    f,
		name: fn,
		//nolint:unconvert
		dev: uint64(stat.Dev),
		ino: stat.Ino,
		fd:  fd,
	}

	// check /proc just in case
	if _, err := os.Stat(h.procPath()); err != nil {
		f.Close()
		return nil, errors.Wrapf(err, "couldn't stat %v", h.procPath())
	}

	return h, nil
}

func (h *handle) Close() error {
	h.closeOnce.Do(func() {
		h.f.Close()
	})
	return nil
}

func (h *handle) OpenFifo() (io.WriteCloser, error) {
	path, err := h.path()
	if err != nil {
		logrus.Errorf("get fifo proc path for task log error, fn = %s", h.name)
		return nil, err
	}
	logrus.Debugf("open fifo by proc path for task log, fn = %s, path = %s", h.name, path)

	file, err := os.OpenFile(path, syscall.O_WRONLY, 0)
	if err != nil {
		logrus.Errorf("open fifo by proc path for task log error, fn = %s, path = %s", h.name, path)
		return nil, err
	}
	logrus.Debugf("open fifo handle for task log success %s", h.name)
	return file, nil
}

func (h *handle) procPath() string {
	return fmt.Sprintf("/proc/self/fd/%d", h.fd)
}

func (h *handle) path() (string, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(h.procPath(), &stat); err != nil {
		return "", errors.Wrapf(err, "path %v could not be statted", h.procPath())
	}
	//nolint:unconvert
	if uint64(stat.Dev) != h.dev || stat.Ino != h.ino {
		return "", errors.Errorf("failed to verify handle %v/%v %v/%v", stat.Dev, h.dev, stat.Ino, h.ino)
	}
	return h.procPath(), nil
}
