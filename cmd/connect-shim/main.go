package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/containerd/containerd/api/types"
	taskapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/shim"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Get WD error")
	}
	opts, err := common.LoadOpts(context.Background(), wd)
	if err != nil {
		log.Fatalln("load otps error")
	}
	address := getAddress()
	id := getID()

	log.Printf("create connection")
	conn, err := common.Connect(address, shim.AnonReconnectDialer)
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

	switch os.Args[1] {
	case "create":
		log.Printf("create task")
		topts := opts.TaskOptions
		if topts == nil {
			topts = opts.RuntimeOptions
		}
		req := &taskapi.CreateTaskRequest{
			ID:     id,
			Bundle: wd,
			// Stdin:      opts.IO.Stdin,
			// Stdout:     opts.IO.Stdout,
			// Stderr:     opts.IO.Stderr,
			// Terminal:   opts.IO.Terminal,
			Checkpoint: opts.Checkpoint,
			Options:    topts,
		}
		for _, m := range opts.Rootfs {
			req.Rootfs = append(req.Rootfs, &types.Mount{
				Type:    m.Type,
				Source:  m.Source,
				Options: m.Options,
			})
		}

		r, err := taskClient.Create(context.Background(), req)
		if err != nil {
			log.Fatalf("create error, cause = %v", err)
		}
		log.Printf("create success, pid = %v", r.Pid)
	case "shutdown":
		log.Printf("shutdown")
		_, err = taskClient.Shutdown(context.Background(), &taskapi.ShutdownRequest{
			ID: id,
		})
		if err != nil {
			log.Fatalf("create error, cause = %v", err)
		}
		log.Printf("shutdown shim success")
	case "start":
		log.Printf("start %s", id)
		resp, err := taskClient.Start(context.Background(), &taskapi.StartRequest{
			ID: id,
		})
		if err != nil {
			log.Fatalf("start error, cause = %v", err)
		}
		log.Printf("start success, resp = %v", resp)
	case "delete":
		log.Printf("delete")
		deleteResp, err := taskClient.Delete(context.Background(), &taskapi.DeleteRequest{
			ID: id,
		})
		if err != nil {
			log.Fatalf("delete task error, cause = %v", err)
		}
		log.Printf("delete task success, resp = %v", deleteResp)
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
	address, err := common.ReceiveAddressOverFifo(context.Background(), wd)
	if err != nil {
		log.Fatalln("Get Address error")
	}
	log.Printf("address=%s\n", address)
	return address
}
