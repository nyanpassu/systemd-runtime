package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"

	"github.com/containerd/containerd/sys/reaper"
	"github.com/containerd/go-runc"
	"github.com/sirupsen/logrus"
)

func main() {
	// ctx, cancel := context.WithTimeout(context.Background(), time.Duration(6)*time.Second)
	// defer cancel()
	// cmd := exec.CommandContext(ctx, "sleep", "6")
	// cmd.Start()
	ch := make(chan os.Signal, 32)
	signal.Notify(ch, unix.SIGCHLD)

	ch2 := make(chan os.Signal, 32)
	signal.Notify(ch2, unix.SIGINT, unix.SIGTERM)
	go handleTermSignals(ch2)

	go handleSignals(ch)
	run()
}

func run() {
	monitor := reaper.Default
	ec := monitor.Subscribe()
	go proc(ec)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(10)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sleep", "6")

	logrus.Info("Start")
	exit, err := monitor.Start(cmd)
	if err != nil {
		logrus.Fatalln("Start failed")
	}
	logrus.Info("Wait")
	r, err := monitor.Wait(cmd, exit)
	if err != nil {
		logrus.Fatalln("Start failed")
	}
	logrus.Info("result %v", r)
}

func proc(ch chan runc.Exit) {
	for ex := range ch {
		logrus.WithField(
			"Pid", ex.Pid,
		).WithField(
			"Status", ex.Status,
		).WithField(
			"Timestamp", ex.Timestamp,
		).Info("Process Exit")
	}
}

func handleSignals(signals chan os.Signal) {
	logrus.Info("starting signal loop")

	for sig := range signals {
		switch sig {
		case unix.SIGCHLD:
			if err := reaper.Reap(); err != nil {
				logrus.WithError(err).Error("reap exit status")
			}
		}
	}
}

func handleTermSignals(signals chan os.Signal) {
	logrus.Info("starting handle term signal loop")

	count := 0

	for sig := range signals {
		switch sig {
		case unix.SIGTERM, unix.SIGINT:
			count++
			logrus.WithField("count", count).Info("Term")
			if count == 3 {
				logrus.Exit(0)
			}
		}
	}
}
