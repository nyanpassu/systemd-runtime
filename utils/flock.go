package utils

import (
	"io"
	"os"
	"syscall"
)

type Flock struct {
	file *os.File
}

func NewFlock(file string, flag int, perm os.FileMode) (r Flock, err error) {
	f, err := os.OpenFile(file, flag, perm)
	if err != nil {
		return r, err
	}
	r.file = f
	return
}

func (l Flock) Lock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX)
}

func (l Flock) Unlock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}

func (l Flock) Close() error {
	return l.file.Close()
}

func (l Flock) Read() ([]byte, error) {
	var size int
	if info, err := l.file.Stat(); err == nil {
		size64 := info.Size()
		if int64(int(size64)) == size64 {
			size = int(size64)
		}
	}
	size++

	if size < 512 {
		size = 512
	}

	data := make([]byte, 0, size)
	for {
		if len(data) >= cap(data) {
			d := append(data[:cap(data)], 0)
			data = d[:len(data)]
		}
		n, err := l.file.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
	}
}

func (l Flock) Write(val []byte) error {
	if err := l.file.Truncate(0); err != nil {
		return err
	}
	_, err := l.file.Write(val)
	return err
}
