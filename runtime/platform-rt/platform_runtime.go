package containerd

import (
	"context"
	"os"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
	"github.com/nyanpassu/containerd-eru-runtime-plugin/common"

	"github.com/projecteru2/systemd-runtime/store"
	"github.com/projecteru2/systemd-runtime/systemd"
	"github.com/projecteru2/systemd-runtime/task"
)

// New task manager for v2 shims
func New(
	ctx context.Context,
	root, state,
	containerdAddress, containerdTTRPCAddress string,
	events *exchange.Exchange,
	cs containers.Store,
) (runtime.PlatformRuntime, error) {
	for _, d := range []string{root, state} {
		if err := os.MkdirAll(d, 0711); err != nil {
			return nil, err
		}
	}
	m := &taskManager{}
	if err := initTaskManager(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func initTaskManager(ctx context.Context, m *taskManager) error {
	return nil
}

// TaskManager manages v2 shim's and their tasks
type taskManager struct {
	root                   string
	state                  string
	containerdAddress      string
	containerdTTRPCAddress string

	cs containers.Store
	ts store.TaskStore
	um systemd.UnitManager
	tb task.TaskBuilder
}

// ID of the runtime
func (m *taskManager) ID() string {
	return common.RuntimeName
}

// Create creates a task with the provided id and options.
func (m *taskManager) Create(ctx context.Context, id string, opts runtime.CreateOpts) (runtime.Task, error) {
	bundle, err := newBundle(ctx, m.root, m.state, id, opts.Spec.Value)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			bundle.Delete()
		}
	}()
	err = bundle.SaveOpts(ctx, opts)
	if err != nil {
		return nil, err
	}
	topts := opts.TaskOptions
	if topts == nil {
		topts = opts.RuntimeOptions
	}

	t := store.Task{
		ID:         id,
		BundlePath: bundle.Path(),
		Namespace:  bundle.Namespace(),
	}
	if err = m.ts.Create(ctx, &t); err != nil {
		return nil, err
	}

	unit, err := m.um.Create(ctx, task.UnitName(id), detail(bundle))
	if err != nil {
		return nil, err
	}

	ta, err := m.tb.CreateNewTask(ctx, t, unit)
	if err != nil {
		return nil, err
	}
	return ta, nil
}

// Get returns a task.
func (m *taskManager) Get(ctx context.Context, id string) (runtime.Task, error) {
	t := store.Task{ID: id}
	if err := m.ts.Retrieve(ctx, &t); err != nil {
		return nil, err
	}

	u, err := m.um.Get(ctx, task.UnitName(id))
	if err != nil {
		return nil, err
	}

	ta, err := m.tb.CreateFromRecord(ctx, t, u)
	if err != nil {
		return nil, err
	}
	return ta, nil
}

// Tasks returns all the current tasks for the runtime.
// Any container runs at most one task at a time.
func (m *taskManager) Tasks(ctx context.Context, all bool) ([]runtime.Task, error) {
	tasks, err := m.ts.RetrieveAll(ctx)
	if err != nil {
		return nil, err
	}
	if !all {
		namespace, err := namespaces.NamespaceRequired(ctx)
		if err != nil {
			return nil, err
		}

		var ts []store.Task
		for _, t := range tasks {
			if t.Namespace == namespace {
				ts = append(ts, t)
			}
		}
		tasks = ts
	}
	var ts []runtime.Task
	for _, t := range tasks {
		u, err := m.um.Get(ctx, task.UnitName(t.ID))
		if err != nil {
			return nil, err
		}
		task, err := m.tb.CreateFromRecord(ctx, t, u)
		if err != nil {
			return nil, err
		}
		ts = append(ts, task)
	}
	return ts, nil
}
