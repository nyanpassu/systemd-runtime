package runtime

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"

	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/bundle"
	"github.com/projecteru2/systemd-runtime/common"
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
	m := &taskManager{
		root:                   root,
		state:                  state,
		containerdAddress:      containerdAddress,
		containerdTTRPCAddress: containerdTTRPCAddress,

		containers: cs,
		events:     events,
		tasks:      NewTaskList(),
	}
	if err := m.loadingTaskByNamespaces(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// TaskManager manages v2 shim's and their tasks
type taskManager struct {
	root                   string
	state                  string
	containerdAddress      string
	containerdTTRPCAddress string

	containers containers.Store
	events     *exchange.Exchange
	tasks      TaskList
}

// ID of the runtime
func (m *taskManager) ID() string {
	return common.RuntimeName
}

// Create creates a task with the provided id and options.
func (m *taskManager) Create(ctx context.Context, id string, opts runtime.CreateOpts) (runtime.Task, error) {
	log.G(ctx).WithField("id", id).Info("Create Task")
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	b, err := bundle.NewBundle(ctx, m.root, m.state, id, ns, m.containerdAddress, m.containerdTTRPCAddress, m.events, opts)
	if err != nil {
		return nil, err
	}
	ta, err := b.LoadTask(ctx, m.events)
	if err != nil {
		return nil, err
	}
	m.addTask(ns, ta)
	return ta, nil
}

// Get returns a task.
func (m *taskManager) Get(ctx context.Context, id string) (runtime.Task, error) {
	log.G(ctx).WithField("id", id).Debug("Get Task")
	t, err := m.tasks.Get(ctx, id)
	if err != nil {
		log.G(ctx).WithField("id", id).WithError(err).Error("get task error")
		return nil, err
	}
	log.G(ctx).WithField("id", t.ID()).Debug("Task Got")
	return t, nil
}

// Tasks returns all the current tasks for the runtime.
// Any container runs at most one task at a time.
func (m *taskManager) Tasks(ctx context.Context, all bool) ([]runtime.Task, error) {
	return m.tasks.GetAll(ctx, all)
}

// Add adds a task into runtime.
func (m *taskManager) Add(ctx context.Context, task runtime.Task) error {
	return nil
}

// Delete remove a task.
func (m *taskManager) Delete(ctx context.Context, taskID string) {}

func (m *taskManager) container(ctx context.Context, id string) (*containers.Container, error) {
	container, err := m.containers.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &container, nil
}

func (m *taskManager) loadingTaskByNamespaces(ctx context.Context) error {
	log.G(ctx).Debug("loading task by namespaces")
	defer log.G(ctx).Debug("done loading task by namespaces")

	nsDirs, err := ioutil.ReadDir(m.state)
	if err != nil {
		return err
	}
	for _, nsd := range nsDirs {
		if !nsd.IsDir() {
			continue
		}
		ns := nsd.Name()
		// skip hidden directories
		if len(ns) > 0 && ns[0] == '.' {
			continue
		}
		log.G(ctx).WithField("namespace", ns).Debug("loading tasks in namespace")
		if err := m.loadingTasks(namespaces.WithNamespace(ctx, ns)); err != nil {
			log.G(ctx).WithField("namespace", ns).WithError(err).Error("loading tasks in namespace")
			continue
		}
	}
	return nil
}

func (m *taskManager) loadingTasks(ctx context.Context) error {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return err
	}
	log.G(ctx).WithField("namespace", ns).Info("loading task")

	shimDirs, err := ioutil.ReadDir(filepath.Join(m.state, ns))
	if err != nil {
		return err
	}
	for _, sd := range shimDirs {
		if !sd.IsDir() {
			continue
		}
		id := sd.Name()
		// skip hidden directories
		if len(id) > 0 && id[0] == '.' {
			continue
		}
		if err := m.loadingTask(ctx, id, ns); err != nil {
			log.G(ctx).WithError(err).Error("loading task error")
		}
	}
	return nil
}

func (m *taskManager) loadingTask(ctx context.Context, id, ns string) (err error) {
	b, err := bundle.LoadBundle(ctx, m.state, id, ns, m.events)
	if err != nil {
		logger := log.G(ctx).WithField(
			"state", m.state,
		).WithField(
			"id", id,
		).WithField(
			"namespace", ns,
		)
		logger.WithError(err).Error("loading bundle error")
		if err := bundle.DeleteBundle(m.state, id, ns); err != nil {
			logger.WithError(err).Error("delete bundle error")
		}
		return
	}

	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Error("loading task error, delete bundle")

			if err := b.Delete(ctx); err != nil {
				log.G(ctx).WithField(
					"state", m.state,
				).WithField(
					"id", id,
				).WithField(
					"namespace", ns,
				).WithError(
					err,
				).Error(
					"delete bundle error",
				)
			}
		}
	}()

	var (
		container *containers.Container
		ta        runtime.Task
	)
	container, err = m.container(ctx, id)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return errors.WithMessage(err, "container not exists")
		}
		return errors.WithMessage(err, "get container error")
	}

	log.G(ctx).WithField(
		"id", container.ID,
	).WithField(
		"runtimeName", container.Runtime.Name,
	).Debug(
		"container exists",
	)

	ta, err = b.LoadTask(ctx, m.events)
	if err != nil {
		return errors.WithMessage(err, "loading task from bundle error")
	}
	if _, err := m.addTask(ns, ta); err != nil {
		return errors.WithMessage(err, "adding task error")
	}
	return nil
}

func (m *taskManager) addTask(namespace string, t runtime.Task) (runtime.Task, error) {
	ta := taskWrap{
		Task:  t,
		tasks: &m.tasks,
	}
	return ta, m.tasks.AddWithNamespace(namespace, ta)
}
