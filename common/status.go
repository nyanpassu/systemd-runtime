package common

import (
	"os"

	"github.com/containerd/containerd/runtime"
)

const (
	shimStatusFileName = "shim.status"
	shimLockFileName   = "shim.lock"
)

type ShimStatus struct {
	PID      int
	Created  bool
	Disabled bool
	Exit     *runtime.Exit
}

func OpenShimStatusFile(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+shimStatusFileName, os.O_RDWR|os.O_CREATE, 0666)
}

func OpenShimLockFile(bundlePath string) (*os.File, error) {
	return os.OpenFile(bundlePath+"/"+shimLockFileName, os.O_RDWR|os.O_CREATE, 0666)
}
