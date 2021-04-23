package main

import (
	"context"
	"io/ioutil"
	"log"
	"syscall"

	"github.com/containerd/fifo"
)

func main() {
	fifo, err := fifo.OpenFifo(context.Background(), "/home/parallels/go/src/github.com/projecteru2/systemd-runtime/fifo", syscall.O_CREAT|syscall.O_RDONLY, 777)
	if err != nil {
		log.Fatalln(err)
	}

	content, err := ioutil.ReadAll(fifo)
	if err != nil {
		log.Fatalln(err)
	}
	fifo.Close()

	log.Println(string(content))
}
