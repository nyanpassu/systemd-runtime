package main

import (
	"context"

	"github.com/projecteru2/systemd-runtime/runshim"
	// eruShim "github.com/projecteru2/systemd-runtime/runtime/shim"
	v2 "github.com/containerd/containerd/runtime/v2/runc/v2"
	cmdShim "github.com/projecteru2/systemd-runtime/cmd/eru-systemd-shim/shim"
)

func main() {
	// init and execute the shim
	runshim.Run("io.containerd.systemd.v1", newShim)
}

func newShim(ctx context.Context, id string, publisher runshim.Publisher, shutdown func()) (runshim.Shim, error) {
	taskService, err := v2.New(ctx, id, publisher, shutdown)
	if err != nil {
		return nil, err
	}
	return cmdShim.Shim{ID: id, TaskService: taskService}, nil
}
