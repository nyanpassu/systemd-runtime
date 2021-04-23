package main

import (
	"context"
	"log"
	"syscall"

	"github.com/containerd/fifo"
)

func main() {
	fifo, err := fifo.OpenFifo(context.Background(), "/home/parallels/go/src/github.com/projecteru2/systemd-runtime/fifo", syscall.O_CREAT|syscall.O_WRONLY, 777)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = fifo.Write([]byte("diucat"))
	if err != nil {
		log.Fatalln(err)
	}
	fifo.Close()
}
