package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"

	"github.com/projecteru2/systemd-runtime/runtime/v2/runc"
)

const optionsFilename = "options.json"

// Container .
type Container struct {
	mu sync.Mutex

	// ID of the container
	ID string
	// Bundle path
	Bundle  string
	runtime runc.Runc

	status task.Status
	end    chan struct{}
	pid    int

	// cgroup is either cgroups.Cgroup or *cgroupsv2.Manager
	// cgroup          interface{}
	// process         process.Process
	// processes       map[string]process.Process
	// reservedProcess map[string]struct{}
}

// NewContainer returns a new runc container
func NewContainer(ctx context.Context, runtime runc.Runc, r *taskAPI.CreateTaskRequest) (*Container, error) {
	// ns, err := namespaces.NamespaceRequired(ctx)
	// if err != nil {
	// 	return nil, errors.Maskf(err, "create namespace")
	// }

	// var opts options.Options
	// if r.Options != nil {
	// 	v, err := typeurl.UnmarshalAny(r.Options)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	opts = *v.(*options.Options)
	// }

	// var mounts []process.Mount
	// for _, m := range r.Rootfs {
	// 	mounts = append(mounts, process.Mount{
	// 		Type:    m.Type,
	// 		Source:  m.Source,
	// 		Target:  m.Target,
	// 		Options: m.Options,
	// 	})
	// }

	// rootfs := ""
	// if len(mounts) > 0 {
	// 	rootfs = filepath.Join(r.Bundle, "rootfs")
	// 	if err := os.Mkdir(rootfs, 0711); err != nil && !os.IsExist(err) {
	// 		return nil, err
	// 	}
	// }

	// config := &process.CreateConfig{
	// 	ID:               r.ID,
	// 	Bundle:           r.Bundle,
	// 	Runtime:          opts.BinaryName,
	// 	Rootfs:           mounts,
	// 	Terminal:         r.Terminal,
	// 	Stdin:            r.Stdin,
	// 	Stdout:           r.Stdout,
	// 	Stderr:           r.Stderr,
	// 	Checkpoint:       r.Checkpoint,
	// 	ParentCheckpoint: r.ParentCheckpoint,
	// 	Options:          r.Options,
	// }

	// if err := WriteOptions(r.Bundle, opts); err != nil {
	// 	return nil, err
	// }
	// // For historical reason, we write opts.BinaryName as well as the entire opts
	// if err := WriteRuntime(r.Bundle, opts.BinaryName); err != nil {
	// 	return nil, err
	// }
	// defer func() {
	// 	if err != nil {
	// 		if err2 := mount.UnmountAll(rootfs, 0); err2 != nil {
	// 			logrus.WithError(err2).Warn("failed to cleanup rootfs mount")
	// 		}
	// 	}
	// }()
	// for _, rm := range mounts {
	// 	m := &mount.Mount{
	// 		Type:    rm.Type,
	// 		Source:  rm.Source,
	// 		Options: rm.Options,
	// 	}
	// 	if err := m.Mount(rootfs); err != nil {
	// 		return nil, errors.Maskf(err, "failed to mount rootfs component %v", m)
	// 	}
	// }

	// var p process.Process
	// if p, err = process.CreateInit(
	// 	ctx,
	// 	r.Bundle,
	// 	filepath.Join(r.Bundle, "work"),
	// 	ns,
	// 	platform,
	// 	config,
	// 	&opts,
	// 	rootfs,
	// ); err != nil {
	// 	return nil, errdefs.ToGRPC(err)
	// }
	if err := runtime.Create(ctx, r.ID, r.Bundle, &runc.CreateOpts{
		PidFile: "shim.pid",
	}); err != nil {
		return nil, err
	}
	// pid, err := readPid(r.ID, r.Bundle)
	// if err != nil {
	// 	return nil, err
	// }

	container := &Container{
		ID:      r.ID,
		Bundle:  r.Bundle,
		status:  task.StatusRunning,
		end:     make(chan struct{}),
		runtime: runtime,
		// pid:     pid,
		// process:         p,
		// processes:       make(map[string]process.Process),
		// reservedProcess: make(map[string]struct{}),
	}
	// pid := p.Pid()
	// if pid > 0 {
	// 	var cg interface{}
	// 	if cgroups.Mode() == cgroups.Unified {
	// 		g, err := cgroupsv2.PidGroupPath(pid)
	// 		if err != nil {
	// 			logrus.WithError(err).Errorf("loading cgroup2 for %d", pid)
	// 			return container, nil
	// 		}
	// 		cg, err = cgroupsv2.LoadManager("/sys/fs/cgroup", g)
	// 		if err != nil {
	// 			logrus.WithError(err).Errorf("loading cgroup2 for %d", pid)
	// 		}
	// 	} else {
	// 		cg, err = cgroups.Load(cgroups.V1, cgroups.PidPath(pid))
	// 		if err != nil {
	// 			logrus.WithError(err).Errorf("loading cgroup for %d", pid)
	// 		}
	// 	}
	// 	container.cgroup = cg
	// }
	return container, nil
}

// Status .
func (c *Container) Delete(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.runtime.Delete(ctx, c.ID, nil)
}

// Status .
func (c *Container) Status() task.Status {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.status
}

// Start .
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.runtime.Start(ctx, c.ID); err != nil {
		return err
	}

	c.status = task.StatusRunning
	return nil
}

// Wait .
func (c *Container) Wait() <-chan struct{} {
	return c.end
}

// Kill .
func (c *Container) Kill(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.status == task.StatusStopped {
		return nil
	}
	c.status = task.StatusStopped
	close(c.end)
	return nil
}

func readPid(id string, bundle string) (int, error) {
	content, err := os.ReadFile(pidFile(id, bundle))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(content))
}

func pidFile(id string, bundle string) string {
	return fmt.Sprintf("%s/%s/shim.pid", bundle, id)
}

// // Pid of the main process of a container
// func (c *Container) Pid() int {
// 	c.mu.Lock()
// 	defer c.mu.Unlock()
// 	return c.process.Pid()
// }

// // HasPid returns true if the container owns a specific pid
// func (c *Container) HasPid(pid int) bool {
// 	if c.Pid() == pid {
// 		return true
// 	}
// 	for _, p := range c.All() {
// 		if p.Pid() == pid {
// 			return true
// 		}
// 	}
// 	return false
// }

// // All processes in the container
// func (c *Container) All() (o []process.Process) {
// 	c.mu.Lock()
// 	defer c.mu.Unlock()

// 	for _, p := range c.processes {
// 		o = append(o, p)
// 	}
// 	if c.process != nil {
// 		o = append(o, c.process)
// 	}
// 	return o
// }

// ReadOptions reads the option information from the path.
// When the file does not exist, ReadOptions returns nil without an error.
func ReadOptions(path string) (*options.Options, error) {
	filePath := filepath.Join(path, optionsFilename)
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var opts options.Options
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil, err
	}
	return &opts, nil
}

// // WriteOptions writes the options information into the path
// func WriteOptions(path string, opts options.Options) error {
// 	data, err := json.Marshal(opts)
// 	if err != nil {
// 		return err
// 	}
// 	return ioutil.WriteFile(filepath.Join(path, optionsFilename), data, 0600)
// }

// // WriteRuntime writes the runtime information into the path
// func WriteRuntime(path, runtime string) error {
// 	return ioutil.WriteFile(filepath.Join(path, "runtime"), []byte(runtime), 0600)
// }

// ReadRuntime reads the runtime information from the path
func ReadRuntime(path string) (string, error) {
	data, err := ioutil.ReadFile(filepath.Join(path, "runtime"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// // ShouldKillAllOnExit reads the bundle's OCI spec and returns true if
// // there is an error reading the spec or if the container has a private PID namespace
// func ShouldKillAllOnExit(ctx context.Context, bundlePath string) bool {
// 	var bundleSpec specs.Spec
// 	bundleConfigContents, err := ioutil.ReadFile(filepath.Join(bundlePath, "config.json"))
// 	if err != nil {
// 		log.G(ctx).WithError(err).Error("shouldKillAllOnExit: failed to read config.json")
// 		return true
// 	}
// 	if err := json.Unmarshal(bundleConfigContents, &bundleSpec); err != nil {
// 		log.G(ctx).WithError(err).Error("shouldKillAllOnExit: failed to unmarshal bundle json")
// 		return true
// 	}
// 	if bundleSpec.Linux != nil {
// 		for _, ns := range bundleSpec.Linux.Namespaces {
// 			if ns.Type == specs.PIDNamespace && ns.Path == "" {
// 				return false
// 			}
// 		}
// 	}
// 	return true
// }
