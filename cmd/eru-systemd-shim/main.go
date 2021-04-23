package main

import (
	"context"

	"github.com/projecteru2/systemd-runtime/runshim"
	eruShim "github.com/projecteru2/systemd-runtime/runtime/shim"
)

func main() {
	// init and execute the shim
	runshim.Run("io.containerd.systemd.v1", newShim)
}

func newShim(ctx context.Context, id string, publisher runshim.Publisher, shutdown func()) (runshim.Shim, error) {
	taskService, err := eruShim.New(ctx, id, publisher, shutdown)
	if err != nil {
		return nil, err
	}
	return shim{TaskService: taskService}, nil
}
