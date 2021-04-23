package platformrt

import (
	"github.com/containerd/containerd/runtime"

	"github.com/projecteru2/systemd-runtime/systemd"
)

type loadingFailedTask struct {
	runtime.Task
}

type pendingTask struct {
	unit *systemd.Unit
	runtime.Task
}
