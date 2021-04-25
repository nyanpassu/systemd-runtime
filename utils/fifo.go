package utils

import (
	"context"
	"io/ioutil"
	"os"
	"syscall"

	"github.com/containerd/fifo"
)

const fifo_file_name = "fifo_address"

func SendAddressOverFifo(ctx context.Context, bundle string, address string) error {
	fifo, err := fifo.OpenFifo(context.Background(), bundle+"/"+fifo_file_name, syscall.O_CREAT|syscall.O_WRONLY, 777)
	if err != nil {
		return err
	}
	defer fifo.Close()
	defer os.Remove(bundle + "/" + fifo_file_name)

	_, err = fifo.Write([]byte(address))
	return err
}

func ReceiveAddressOverFifo(ctx context.Context, bundle string) (string, error) {
	fifo, err := fifo.OpenFifo(context.Background(), bundle+"/"+fifo_file_name, syscall.O_CREAT|syscall.O_RDONLY, 777)
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
