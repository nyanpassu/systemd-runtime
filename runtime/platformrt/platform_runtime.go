package platformrt

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
	"github.com/nyanpassu/containerd-eru-runtime-plugin/common"

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
	m := &taskManager{
		root:                   root,
		state:                  state,
		containerdAddress:      containerdAddress,
		containerdTTRPCAddress: containerdTTRPCAddress,

		containers:      cs,
		events:          events,
		tasks:           task.NewTaskList(),
		launcherFactory: &launcherFactory{um: systemd.NewUnitManager("/lib/systemd/system")},
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
	// ts              store.TaskStore
	events          *exchange.Exchange
	launcherFactory task.TaskLauncherFactory
	tasks           task.Tasks
	// systemdManager  *systemd.UnitManager
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

	// t := store.Task{
	// 	ID:         id,
	// 	BundlePath: bundle.Path(),
	// 	Namespace:  bundle.Namespace(),
	// }
	// if err = m.ts.Create(ctx, &t); err != nil {
	// 	return nil, err
	// }

	launcher := m.launcherFactory.NewLauncher(ctx, bundle, opts.Runtime, m.containerdAddress, m.containerdTTRPCAddress, m.events, m.tasks)
	ta, err := launcher.Create(ctx, opts)

	if err != nil {
		return nil, err
	}
	return ta, nil
}

// Get returns a task.
func (m *taskManager) Get(ctx context.Context, id string) (runtime.Task, error) {
	return m.tasks.Get(ctx, id)
}

// Tasks returns all the current tasks for the runtime.
// Any container runs at most one task at a time.
func (m *taskManager) Tasks(ctx context.Context, all bool) ([]runtime.Task, error) {
	return m.tasks.GetAll(ctx, all)
}

func (m *taskManager) container(ctx context.Context, id string) (*containers.Container, error) {
	container, err := m.containers.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &container, nil
}

func (m *taskManager) loadingTaskByNamespaces(ctx context.Context) error {
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

		bundle, err := loadAndCheckBundle(ctx, m, id)
		if err != nil {
			m.tasks.Add(ctx, &loadingFailedTask{})
			continue
		}

		// unit, err := m.systemdManager.Get(ctx, name(id))
		// if err != nil {
		// 	if err == systemd.ErrUnitNotExists {
		// 		_ = bundle.Delete()
		// 		continue
		// 	}
		// 	m.tasks.Add(ctx, loadingFailedTask{})
		// 	continue
		// }

		container, err := m.container(ctx, id)
		if err != nil {
			log.G(ctx).WithError(err).Errorf("loading container %s", id)
			// if err := mount.UnmountAll(filepath.Join(bundle.Path(), "rootfs"), 0); err != nil {
			// 	log.G(ctx).WithError(err).Errorf("forceful unmount of rootfs %s", id)
			// }
			launcher := m.launcherFactory.NewLauncher(ctx, bundle, container.Runtime.Name, m.containerdAddress, m.containerdTTRPCAddress, m.events, m.tasks)
			if _, err = launcher.Delete(ctx); err != nil {
				log.G(ctx).WithError(err).Errorf("delete task resource %s", id)
			}
			continue
		}

		launcher := m.launcherFactory.NewLauncher(ctx, bundle, container.Runtime.Name, m.containerdAddress, m.containerdTTRPCAddress, m.events, m.tasks)
		ta, err := launcher.LoadAsync(ctx)
		if err != nil {
			log.G(ctx).WithError(err).Errorf("loading container %s", id)
			ta = &loadingFailedTask{}
		}
		m.tasks.Add(ctx, ta)
	}
	return nil
}

func loadAndCheckBundle(ctx context.Context, m *taskManager, id string) (task.Bundle, error) {
	bundle, err := loadBundle(ctx, m.state, id)
	if err != nil {
		return nil, err
	}
	// fast path
	bf, err := ioutil.ReadDir(bundle.Path())
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fast path read bundle path for %s", bundle.Path)
		return nil, err
	}
	if len(bf) == 0 {
		return nil, errors.New("bundle is empty")
	}
	return bundle, nil
}
