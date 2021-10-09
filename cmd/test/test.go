package main

import (
	"context"
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"time"

	"github.com/containerd/containerd/runtime"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/projecteru2/systemd-runtime/common"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("getwd error")
	}
	var bundlePath string
	flag.StringVar(&bundlePath, "bundle", wd, "specific bundle path")
	flag.Parse()

	switch flag.Arg(0) {
	case "shim":
		err = shim(context.Background(), bundlePath)
	case "task-manager":
		err = taskManager(context.Background(), bundlePath)
	}
	if err != nil {
		log.Fatalln(err)
	}
}

func waitCTRLD() <-chan struct{} {
	ch := make(chan struct{})

	go func() {
		ioutil.ReadAll(os.Stdin)
		close(ch)
	}()

	return ch
}

func shim(ctx context.Context, bundlePath string) error {
	signals := make(chan os.Signal, 32)
	smp := []os.Signal{unix.SIGTERM, unix.SIGINT}
	signal.Notify(signals, smp...)

	mng, err := common.NewStatusManager(bundlePath, log.NewEntry(log.StandardLogger()))
	if err != nil {
		return err
	}
	status, err := mng.LockForStartShim(ctx)
	if err != nil {
		return err
	}
	log.Infof("created = %v, disabled =%v, pid = %v", status.Created, status.Disabled, status.PID)

	pid := os.Getpid()
	status.Created = true
	status.PID = pid

	err = mng.UpdateStatus(ctx, status)
	if err != nil {
		return err
	}
	err = mng.UnlockStatusFile()
	if err != nil {
		return err
	}

	log.Info("wait termination")
	select {
	case <-signals:
	case <-waitCTRLD():
	}

	ctx = context.Background()
	status, err = mng.LockForUpdateStatus(ctx)
	if err != nil {
		return err
	}
	log.Infof("created = %v, disabled =%v, pid = %v", status.Created, status.Disabled, status.PID)
	status.Exit = &runtime.Exit{
		Pid:       uint32(pid),
		Status:    0,
		Timestamp: time.Now(),
	}
	if err = mng.UpdateStatus(ctx, status); err != nil {
		return err
	}
	return mng.ReleaseLocks()
}

func taskManager(ctx context.Context, bundlePath string) error {
	action := flag.Arg(1)

	mng, err := common.NewStatusManager(bundlePath, log.NewEntry(log.StandardLogger()))
	if err != nil {
		return err
	}
	status, running, err := mng.LockForTaskManager(ctx)
	if err != nil {
		return err
	}
	log.Infof("shimRunning = %v, created = %v, disabled =%v, pid = %v", running, status.Created, status.Disabled, status.PID)
	if status.Exit != nil {
		log.Infof("exitAt = %v", status.Exit.Timestamp)
	}

	switch action {
	case "disable":
		status.Disabled = true
		err = mng.UpdateStatus(ctx, status)
		if err != nil {
			return err
		}
	}
	return mng.UnlockStatusFile()
}
