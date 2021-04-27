package platformrt

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"path/filepath"

	"net"
	// "os"
	gruntime "runtime"
	"time"

	"github.com/containerd/fifo"
	// "github.com/containerd/ttrpc"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	// "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"

	// "github.com/containerd/containerd/pkg/timeout"
	"github.com/containerd/containerd/runtime"
	taskapi "github.com/containerd/containerd/runtime/v2/task"

	"github.com/projecteru2/systemd-runtime/runshim"
	"github.com/projecteru2/systemd-runtime/systemd"
	"github.com/projecteru2/systemd-runtime/task"
	// "github.com/projecteru2/systemd-runtime/utils"
)

type launcherFactory struct {
	um *systemd.UnitManager
}

func (f *launcherFactory) NewLauncher(
	ctx context.Context,
	b task.Bundle,
	runtime, containerdAddress string, containerdTTRPCAddress string,
	events *exchange.Exchange, tasks task.Tasks,
) task.TaskLauncher {
	return &launcher{
		um:                     f.um,
		bundle:                 b,
		runtime:                runtime,
		containerdAddress:      containerdAddress,
		containerdTTRPCAddress: containerdTTRPCAddress,
		events:                 events,
		tasks:                  tasks,
	}
}

type launcher struct {
	um                     *systemd.UnitManager
	bundle                 task.Bundle
	runtime                string
	containerdAddress      string
	containerdTTRPCAddress string
	events                 *exchange.Exchange
	tasks                  task.Tasks
}

func (l *launcher) Create(ctx context.Context, opts runtime.CreateOpts) (_ runtime.Task, err error) {
	args := []string{"-id", l.bundle.ID()}
	if logrus.GetLevel() == logrus.DebugLevel {
		args = append(args, "-debug")
	}
	args = append(args, "systemd-start")

	cmd, err := runshim.SystemdCommand(ctx, l.runtime, l.containerdAddress, l.containerdTTRPCAddress, l.bundle.Path(), nil, args...)
	if err != nil {
		return nil, err
	}
	ExecStart := []string{cmd.CmdPath}
	ExecStart = append(ExecStart, cmd.Args...)
	unit, err := l.um.Create(ctx, name(l.bundle.ID()), systemd.Detail{
		Unit: systemd.UnitSector{
			Description: l.bundle.ID(),
		},
		Service: systemd.ServiceSector{
			Type:             "exec",
			WorkingDirectory: cmd.WorkingPath,
			Environment:      cmd.Env,
			ExecStart:        ExecStart,
		},
		Install: systemd.InstallSector{
			WantedBy: "multi-user.target",
		},
	})
	if err != nil {
		log.G(ctx).WithError(err).Error("create systemd unit error")
		return nil, err
	}
	// log.G(ctx).Info("start unit")
	// if err := unit.Start(ctx); err != nil {
	// 	log.G(ctx).WithError(err).Error("start unit error")
	// 	return nil, err
	// }
	// log.G(ctx).Info("ReceiveAddressOverFifo")
	// addr, err := utils.ReceiveAddressOverFifo(ctx, l.bundle.Path())
	// if err != nil {
	// 	log.G(ctx).WithError(err).Error("ReceiveAddressOverFifo error")
	// 	return nil, err
	// }
	// log.G(ctx).Info("TTRPC Connect")
	// conn, err := runshim.Connect(addr, runshim.AnonReconnectDialer)
	// if err != nil {
	// 	log.G(ctx).WithError(err).Error("TTRPC Connect Error")
	// 	return nil, err
	// }
	// defer func() {
	// 	if err != nil {
	// 		conn.Close()
	// 	}
	// }()
	// client := ttrpc.NewClient(conn)
	// taskClient := taskapi.NewTaskClient(client)

	// log.G(ctx).Info("Connect Shim")
	// _, err = taskClient.Connect(ctx, &taskapi.ConnectRequest{ID: l.bundle.ID()})
	// if err != nil {
	// 	log.G(ctx).WithError(err).Error("Connect Shim Error")
	// 	return nil, err
	// }

	// topts := opts.TaskOptions
	// if topts == nil {
	// 	topts = opts.RuntimeOptions
	// }

	// req := &taskapi.CreateTaskRequest{
	// 	ID:         l.bundle.ID(),
	// 	Bundle:     l.bundle.Path(),
	// 	Stdin:      opts.IO.Stdin,
	// 	Stdout:     opts.IO.Stdout,
	// 	Stderr:     opts.IO.Stderr,
	// 	Terminal:   opts.IO.Terminal,
	// 	Checkpoint: opts.Checkpoint,
	// 	Options:    topts,
	// }
	// for _, m := range opts.Rootfs {
	// 	req.Rootfs = append(req.Rootfs, &types.Mount{
	// 		Type:    m.Type,
	// 		Source:  m.Source,
	// 		Options: m.Options,
	// 	})
	// }

	// log.G(ctx).Info("Create container")
	// createResp, err := taskClient.Create(ctx, req)
	// if err != nil {
	// 	log.G(ctx).WithError(err).Error("Create container failed")
	// 	return nil, err
	// }

	// log.G(ctx).Info("Shutdown Shim")
	// _, err = taskClient.Shutdown(ctx, &taskapi.ShutdownRequest{
	// 	ID: l.bundle.ID(),
	// })
	// if err != nil {
	// 	log.G(ctx).WithError(err).Error("Shutdown Shim Error")
	// 	return nil, err
	// }

	// t := task.NewTask(createResp.Pid, l.bundle, l.events, taskClient, l.tasks, client)
	// if err := l.tasks.Add(ctx, t); err != nil {
	// 	return nil, err
	// }

	// return t, nil

	t := &pendingTask{
		unit:     unit,
		bundle:   l.bundle,
		tasks:    l.tasks,
		launcher: l,
		events:   l.events,
	}
	if err := l.tasks.Add(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (l *launcher) LoadAsync(ctx context.Context) (runtime.Task, error) {
	unit, err := l.um.Get(ctx, name(l.bundle.ID()))
	if err != nil {
		return nil, err
	}
	t := &pendingTask{
		unit:     unit,
		bundle:   l.bundle,
		tasks:    l.tasks,
		events:   l.events,
		launcher: l,
	}
	if err := l.tasks.Add(ctx, t); err != nil {
		return nil, err
	}
	go func() {
		ctx := namespaces.WithNamespace(context.Background(), l.bundle.Namespace())
		_, err := t.loadAndReplaceSelf(ctx)
		if err != nil {
			l.tasks.Add(ctx, &loadingFailedTask{})
			return
		}
	}()
	return t, nil
}

// func (l *launcher) Load(ctx context.Context) (runtime.Task, error) {
// 	address, err := utils.ReceiveAddressOverFifo(ctx, l.bundle.Path())
// 	// loadAddress(filepath.Join(l.bundle.Path(), "address"))
// 	if err != nil {
// 		return nil, err
// 	}
// 	conn, err := runshim.Connect(address, runshim.AnonReconnectDialer)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer func() {
// 		if err != nil {
// 			conn.Close()
// 		}
// 	}()
// 	shimCtx, cancelShimLog := context.WithCancel(ctx)
// 	defer func() {
// 		if err != nil {
// 			cancelShimLog()
// 		}
// 	}()
// 	f, err := openShimLog(shimCtx, l.bundle, runshim.AnonReconnectDialer)
// 	if err != nil {
// 		return nil, errors.Wrap(err, "open shim log pipe")
// 	}
// 	defer func() {
// 		if err != nil {
// 			f.Close()
// 		}
// 	}()
// 	// open the log pipe and block until the writer is ready
// 	// this helps with synchronization of the shim
// 	// copy the shim's logs to containerd's output
// 	// go func() {
// 	// 	defer f.Close()
// 	// 	if _, err := io.Copy(os.Stderr, f); err != nil {
// 	// 		// When using a multi-container shim the 2nd to Nth container in the
// 	// 		// shim will not have a separate log pipe. Ignore the failure log
// 	// 		// message here when the shim connect times out.
// 	// 		if !errors.Is(err, os.ErrNotExist) {
// 	// 			log.G(ctx).WithError(err).Error("copy shim log")
// 	// 		}
// 	// 	}
// 	// }()

// 	onCloseWithShimLog := func(tasks task.Tasks, bundle task.Bundle) func() {
// 		return func() {
// 			go func() {
// 				ctx, cancel := timeout.WithContext(context.Background(), task.LoadTimeout)
// 				defer cancel()
// 				var launcher task.TaskLauncher
// 				tasks.ReplaceWithSupplier(namespaces.WithNamespace(ctx, bundle.Namespace()), bundle.ID(), func(ctx context.Context) runtime.Task {
// 					t, err := launcher.Load(ctx)
// 					if err != nil {
// 						return &loadingFailedTask{}
// 					}
// 					return t
// 				})
// 			}()
// 			cancelShimLog()
// 			f.Close()
// 		}
// 	}(l.tasks, l.bundle)

// 	client := ttrpc.NewClient(conn, ttrpc.WithOnClose(onCloseWithShimLog))
// 	defer func() {
// 		if err != nil {
// 			client.Close()
// 		}
// 	}()
// 	taskClient := taskapi.NewTaskClient(client)

// 	ctx, cancel := timeout.WithContext(ctx, task.LoadTimeout)
// 	defer cancel()
// 	taskPid, err := connect(ctx, taskClient, l.bundle.ID())
// 	if err != nil {
// 		return nil, err
// 	}
// 	return task.NewTask(taskPid, l.bundle, l.events, taskClient, l.tasks, client), nil
// }

func (l *launcher) Delete(ctx context.Context) (*runtime.Exit, error) {
	log.G(ctx).Info("cleaning up dead shim")
	// Windows cannot delete the current working directory while an
	// executable is in use with it. For the cleanup case we invoke with the
	// default work dir and forward the bundle path on the cmdline.
	var bundlePath string
	if gruntime.GOOS != "windows" {
		bundlePath = l.bundle.Path()
	}

	cmd, err := runshim.Command(ctx,
		l.runtime,
		l.containerdAddress,
		l.containerdTTRPCAddress,
		bundlePath,
		nil,
		"-id", l.bundle.ID(),
		"-bundle", l.bundle.Path(),
		"delete")
	if err != nil {
		return nil, err
	}
	var (
		out  = bytes.NewBuffer(nil)
		errb = bytes.NewBuffer(nil)
	)
	cmd.Stdout = out
	cmd.Stderr = errb
	if err := cmd.Run(); err != nil {
		return nil, errors.Wrapf(err, "%s", errb.String())
	}
	s := errb.String()
	if s != "" {
		log.G(ctx).Warnf("cleanup warnings %s", s)
	}
	var response taskapi.DeleteResponse
	if err := response.Unmarshal(out.Bytes()); err != nil {
		return nil, err
	}
	if err := l.bundle.Delete(); err != nil {
		return nil, err
	}
	return &runtime.Exit{
		Status:    response.ExitStatus,
		Timestamp: response.ExitedAt,
		Pid:       response.Pid,
	}, nil
}

func connect(ctx context.Context, taskService taskapi.TaskService, id string) (uint32, error) {
	response, err := taskService.Connect(ctx, &taskapi.ConnectRequest{
		ID: id,
	})
	if err != nil {
		return 0, err
	}
	return uint32(response.TaskPid), nil
}

func loadAddress(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func openShimLog(ctx context.Context, bundle task.Bundle, _ func(string, time.Duration) (net.Conn, error)) (io.ReadCloser, error) {
	return fifo.OpenFifo(ctx, filepath.Join(bundle.Path(), "log"), unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, 0700)
}

func checkCopyShimLogError(ctx context.Context, err error) error {
	// When using a multi-container shim, the fifo of the 2nd to Nth
	// container will not be opened when the ctx is done. This will
	// cause an ErrReadClosed that can be ignored.
	select {
	case <-ctx.Done():
		if err == fifo.ErrReadClosed {
			return nil
		}
	default:
	}
	return err
}
