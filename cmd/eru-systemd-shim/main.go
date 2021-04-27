package main

import (
	"context"

	"github.com/projecteru2/systemd-runtime/runshim"
	// eruShim "github.com/projecteru2/systemd-runtime/runtime/shim"
	cmdShim "github.com/projecteru2/systemd-runtime/cmd/eru-systemd-shim/shim"
	"github.com/projecteru2/systemd-runtime/runtime/shimv2"
)

func main() {
	// init and execute the shim

	runshim.Run("io.containerd.systemd.v1", newShim)
}

func newShim(ctx context.Context, id string, publisher runshim.Publisher, shutdown func()) (runshim.Shim, error) {
	taskService, err := shimv2.New(ctx, id, publisher, shutdown)
	if err != nil {
		return nil, err
	}
	return cmdShim.Shim{TaskService: taskService}, nil
}
