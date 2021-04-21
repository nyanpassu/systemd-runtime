package runc

import (
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
)

// ReadPidFile reads the pid file at the provided path and returns
// the pid or an error if the read and conversion is unsuccessful
func ReadPidFile(path string) (int, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(string(data))
}

// NewNullIO returns IO setup for /dev/null use with runc
func NewNullIO() (IO, error) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	return &nullIO{
		devNull: f,
	}, nil
}

type nullIO struct {
	devNull *os.File
}

func (n *nullIO) Close() error {
	// this should be closed after start but if not
	// make sure we close the file but don't return the error
	n.devNull.Close()
	return nil
}

func (n *nullIO) Stdin() io.WriteCloser {
	return nil
}

func (n *nullIO) Stdout() io.ReadCloser {
	return nil
}

func (n *nullIO) Stderr() io.ReadCloser {
	return nil
}

func (n *nullIO) Set(c *exec.Cmd) {
	// don't set STDIN here
	c.Stdout = n.devNull
	c.Stderr = n.devNull
}

func (n *nullIO) CloseAfterStart() error {
	return n.devNull.Close()
}
