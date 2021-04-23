package task

import (
	"context"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/runtime"
)

type TaskLauncherFactory interface {
	NewLauncher(ctx context.Context, b Bundle, runtime, containerdAddress string, containerdTTRPCAddress string, events *exchange.Exchange, tasks Tasks) TaskLauncher
	// CreateFromRecord(context.Context, store.Task, *systemd.Unit) (runtime.Task, error)
}

type TaskLauncher interface {
	Create(ctx context.Context) (runtime.Task, error)
	Load(ctx context.Context) (runtime.Task, error)
	Delete(ctx context.Context) (*runtime.Exit, error)
}
