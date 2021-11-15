package utils

import (
	"context"
	"io/ioutil"
	"os"
	"syscall"

	"github.com/containerd/fifo"
)

func SendContentOverFifo(ctx context.Context, filePath string, content string) error {
	fifo, err := fifo.OpenFifo(ctx, filePath, syscall.O_CREAT|syscall.O_WRONLY, 0777)
	if err != nil {
		return err
	}
	defer fifo.Close()
	defer os.Remove(filePath)

	_, err = fifo.Write([]byte(content))
	return err
}

func ReceiveContentOverFifo(ctx context.Context, filePath string) (string, error) {
	fifo, err := fifo.OpenFifo(ctx, filePath, syscall.O_CREAT|syscall.O_RDONLY, 0777)
	if err != nil {
		return "", err
	}
	defer fifo.Close()

	content, err := ioutil.ReadAll(fifo)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
