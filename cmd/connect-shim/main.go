package main

import (
	"context"
	"log"
	"os"
	"strings"

	taskapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"

	"github.com/projecteru2/systemd-runtime/runshim"
	"github.com/projecteru2/systemd-runtime/task"
	"github.com/projecteru2/systemd-runtime/utils"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Get WD error")
	}
	opts, err := task.LoadOpts(context.Background(), wd)
	if err != nil {
		log.Fatalln("load otps error")
	}
	address := getAddress()
	id := getID()
	switch os.Args[1] {
	case "create":
		log.Printf("create connection")
		conn, err := runshim.Connect(address, runshim.AnonReconnectDialer)
		if err != nil {
			log.Fatalln(err)
		}
		defer conn.Close()
		client := ttrpc.NewClient(conn)
		taskClient := taskapi.NewTaskClient(client)
		log.Printf("connect remote")
		resp, err := taskClient.Connect(context.Background(), &taskapi.ConnectRequest{ID: id})
		if err != nil {
			log.Fatalf("connect error, cause = %v", err)
		}
		log.Printf("conn resp = %v", resp)

		log.Printf("create task")
		r, err := taskClient.Create(context.Background(), &taskapi.CreateTaskRequest{
			ID:         id,
			Bundle:     wd,
			Stdin:      opts.IO.Stdin,
			Stdout:     opts.IO.Stdout,
			Stderr:     opts.IO.Stderr,
			Terminal:   opts.IO.Terminal,
			Checkpoint: opts.Checkpoint,
			Options:    opts.RuntimeOptions,
		})
		if err != nil {
			log.Fatalf("create error, cause = %v", err)
		}
		log.Printf("create success, pid = %v", r.Pid)
	default:
		log.Println(id)
	}
}

func getID() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Get WD error")
	}
	lastIndex := strings.LastIndex(wd, "/")
	return wd[lastIndex+1:]
}

func getAddress() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Get WD error")
	}
	address, err := utils.ReceiveAddressOverFifo(context.Background(), wd)
	if err != nil {
		log.Fatalln("Get Address error")
	}
	log.Printf("address=%s\n", address)
	return address
}
