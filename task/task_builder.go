package task

import (
	"context"

	"github.com/containerd/containerd/runtime"

	"github.com/projecteru2/systemd-runtime/store"
	"github.com/projecteru2/systemd-runtime/systemd"
)

type TaskBuilder interface {
	CreateNewTask(context.Context, store.Task, *systemd.Unit) (runtime.Task, error)
	CreateFromRecord(context.Context, store.Task, *systemd.Unit) (runtime.Task, error)
}
