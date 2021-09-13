package common

import (
	"os"

	"github.com/containerd/containerd/runtime"
)

const (
	bundleStatusFileName = "status.lock"
	exitStatusFileName   = "exit.lock"
)

type BundleStatus struct {
	PID      int
	Created  bool
	Disabled bool
}

func OpenBundleStatus(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+bundleStatusFileName, os.O_RDWR|os.O_CREATE, 0666)
}

type ExitStatus struct {
	runtime.Exit
}

func OpenExitStatus(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+exitStatusFileName, os.O_RDWR|os.O_CREATE, 0666)
}
