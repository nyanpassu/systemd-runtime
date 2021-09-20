package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/runtime"
)

type ExitStatus struct {
	runtime.Exit
}

func main() {

	pid := os.Getpid()
	logrus.Infof("pid = %d", pid)

	signals := make(chan os.Signal, 32)
	signal.Notify(signals, []os.Signal{unix.SIGTERM, unix.SIGINT}...)

	shouldUnlock := new(int32)
	shouldExit := new(int32)
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	go func() {
		for sig := range signals {
			switch sig {
			case unix.SIGTERM:
				logrus.Info("SIGTERM")
				if atomic.LoadInt32(shouldUnlock) == 1 {
					atomic.StoreInt32(shouldUnlock, 2)
					close(ch1)
				}
				if atomic.LoadInt32(shouldExit) == 1 {
					atomic.StoreInt32(shouldExit, 2)
					close(ch2)
				}
			case unix.SIGINT:
				logrus.Info("SIGINT")
				if atomic.LoadInt32(shouldUnlock) == 1 {
					atomic.StoreInt32(shouldUnlock, 2)
					close(ch1)
				}
				if atomic.LoadInt32(shouldExit) == 1 {
					atomic.StoreInt32(shouldExit, 2)
					close(ch2)
				}
			}
		}
	}()

	var (
		file *os.File
	)
	file, err := os.OpenFile("test.lock", os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		logrus.WithError(err).Fatalln("open file failed")
	}

	flag.Parse()
	action := flag.Arg(0)
	switch action {
	case "test":
		test(file)
		return
	case "b":
		block(file)
	case "nb":
		nblock(file)
	case "exit":
		status := ExitStatus{}
		status.Pid = uint32(os.Getpid())
		status.Status = 0
		status.Timestamp = time.Now()
		content, err := json.Marshal(&status)
		if err != nil {
			logrus.WithError(err).Fatalln("Marshal status error")
		}
		logrus.Info(string(content))
		os.Exit(0)
	default:
		logrus.Fatalf("unrecognized action %s", action)
	}

	atomic.StoreInt32(shouldUnlock, 1)
	<-ch1

	unlock(file)
	logrus.Info("unlocked, press ctrl + c to exit")

	atomic.StoreInt32(shouldExit, 1)
	<-ch2
}

func test(file *os.File) {
	flock_t := &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_GETLK, flock_t); err != nil {
		logrus.WithError(err).Fatalln("test file lock error")
	}
	content, err := json.Marshal(flock_t)
	if err != nil {
		logrus.WithError(err).Fatalln("marshal flock_t error")
	}
	logrus.WithField("CanLock", flock_t.Type == syscall.F_UNLCK).Infof("flock_t = %s", content)
}

func block(file *os.File) {
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLKW, &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	}); err != nil {
		if err == syscall.EINTR {
			logrus.Fatalln("file is already locked, exit")
		}
		logrus.WithError(err).Fatalln("lock file error")
	}
}

func nblock(file *os.File) {
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_WRLCK,
		Whence: io.SeekStart,
	}); err != nil {
		if err == syscall.EAGAIN || err == syscall.EACCES {
			logrus.Fatalln("file is already locked, exit")
		}
		logrus.WithError(err).Fatalln("lock file error")
	}
}

func unlock(file *os.File) {
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &syscall.Flock_t{
		Start:  0,
		Len:    0,
		Type:   syscall.F_UNLCK,
		Whence: io.SeekStart,
	}); err != nil {
		logrus.WithError(err).Fatalln("unlock file error")
	}
}
