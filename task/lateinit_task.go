package task

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"

	"github.com/gogo/protobuf/types"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/systemd"
)

var (
	ErrTaskIsKilled = errors.New("task is killed")
)

type LateInit struct {
	bundle common.Bundle
	unit   *systemd.Unit
	events *exchange.Exchange

	conn   ConnMng
	holder ServiceHolder

	sync.Mutex
	exited *runtime.Exit
}

func NewTask(b common.Bundle, unit *systemd.Unit, events *exchange.Exchange) runtime.Task {
	t := &LateInit{
		bundle: b,
		unit:   unit,
	}
	t.conn.id = t.bundle.ID()
	t.conn.bundlePath = t.bundle.Path()
	t.conn.namespace = t.bundle.Namespace()
	t.conn.holder = &t.holder

	go t.conn.connect()

	return t
}

// ID of the process
func (t *LateInit) ID() string {
	return t.bundle.ID()
}

// State returns the process state
func (t *LateInit) State(ctx context.Context) (runtime.State, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("get task state")

	s, closed := t.holder.getService()

	if s != nil {
		state, err := s.State(ctx)
		if err != nil {
			log.G(ctx).WithField(
				"id", t.ID(),
			).WithError(
				err,
			).Error(
				"get task state error",
			)
		}
		log.G(ctx).WithField(
			"state.Status", StatusIntoString(state.Status),
		).WithField(
			"state.ExitStatus", state.ExitStatus,
		).WithField(
			"state.ExitedAt", state.ExitedAt,
		).Debug(
			"get remote task state success",
		)
		return state, nil
	}

	if closed {
		return runtime.State{
			Status: runtime.StoppedStatus,
		}, nil
	}

	return runtime.State{
		Status: runtime.CreatedStatus,
	}, nil
}

// Kill signals a container
func (t *LateInit) Kill(ctx context.Context, sig uint32, all bool) (err error) {
	logger := log.G(ctx).WithField(
		"id", t.ID(),
	).WithField(
		"signal", sig,
	).WithField(
		"all", all,
	)
	logger.Debug(
		"kill volatile task",
	)
	defer func() {
		if err == nil {
			// t.events.Publish(ctx, runtime.TaskExitEventTopic, &eventstypes.TaskExit{
			// 	ContainerID: t.ID(),
			// 	ID:          t.ID(),
			// 	Pid:         0,
			// 	ExitStatus:  0,
			// 	ExitedAt:    time.Now(),
			// })

			// events.Publish(ctx, runtime.TaskDeleteEventTopic, &eventstypes.TaskDelete{
			//   ContainerID: id,
			//   Pid:         pid,
			//   ExitStatus:  exitStatus,
			//   ExitedAt:    exitedAt,
			// })
		}
	}()

	if err = t.unit.DisableIfPresent(ctx); err != nil {
		return err
	}

	var started bool
	started, err = t.bundle.Disable(ctx)
	if err != nil {
		logger.WithError(err).Error("[LateInit Kill] disable bundle error")
		return err
	}

	if !started {
		t.conn.killNow(sig, all)
		exited := &runtime.Exit{
			Pid:       0,
			Status:    0,
			Timestamp: time.Now(),
		}
		t.setExited(exited)
		return nil
	}

	t.conn.kill(sig, all)
	return t.unit.Stop(ctx)
}

// Pty resizes the processes pty/console
func (t *LateInit) ResizePty(ctx context.Context, size runtime.ConsoleSize) error {
	log.G(ctx).WithField("id", t.ID()).Debug("resize voliatile task pty")

	ta, err := t.getService(ctx)
	if err != nil {
		return err
	}

	return ta.ResizePty(ctx, size)
}

// CloseStdin closes the processes stdin
func (t *LateInit) CloseIO(ctx context.Context) error {
	log.G(ctx).WithField("id", t.ID()).Debug("close task io")

	ta, err := t.getService(ctx)
	if err != nil {
		return err
	}

	return ta.CloseIO(ctx)
}

// Start the container's user defined process
// Consider that our shim is started by it self, so we only need to start the systemd unit, and wait for connection
func (t *LateInit) Start(ctx context.Context) error {
	log.G(ctx).WithField("id", t.ID()).Debug("start task")

	if err := t.unit.Enable(ctx); err != nil {
		log.G(ctx).WithField("id", t.ID()).WithError(err).Error("enable unit error")
		return err
	}

	if err := t.unit.Start(ctx); err != nil {
		log.G(ctx).WithField("id", t.ID()).WithError(err).Error("Start unit error")
		return err
	}

	// wait until the task is started
	supplier, ok := t.holder.getNextService()
	if !ok {
		return ErrTaskIsKilled
	}

	_, err := supplier(ctx)
	return err
}

// Wait for the process to exit
func (t *LateInit) Wait(ctx context.Context) (*runtime.Exit, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("wait task exit")

	ta, err := t.getService(ctx)
	if err == ErrTaskIsKilled {
		log.G(ctx).WithField("id", t.ID()).Warn("task is killed, read task exit status")
		if exited := t.getExited(); exited != nil {
			return exited, nil
		}
		return t.bundle.Exited(ctx)
	}
	if err != nil {
		return nil, err
	}
	return ta.Wait(ctx)
}

// Delete deletes the process
func (t *LateInit) Delete(ctx context.Context) (exit *runtime.Exit, err error) {
	logger := log.G(ctx).WithField("id", t.ID())
	logger.Debug("delete volatile task")

	defer func() {
		if err == nil {
			if err := t.unit.DeleteIfPresent(ctx); err != nil {
				logger.WithField(
					"systemd-unit-name", t.unit.Name,
				).WithError(
					err,
				).Error(
					"delete systemd unit error",
				)
			}
		}
	}()

	ta, err := t.getService(ctx)
	if err == ErrTaskIsKilled {
		if exited := t.getExited(); exited != nil {
			return exited, nil
		}
		return t.bundle.Exited(ctx)
	}
	if err != nil {
		return nil, err
	}
	return ta.Delete(ctx)
}

// PID of the process
func (t *LateInit) PID() uint32 {
	log.G(context.TODO()).WithField("id", t.ID()).Debug("get pid of task")
	service, _ := t.holder.getService()
	if service != nil {
		return service.PID()
	}
	return 0
}

// Namespace that the task exists in
func (t *LateInit) Namespace() string {
	return t.bundle.Namespace()
}

// Pause pauses the container process
func (t *LateInit) Pause(ctx context.Context) error {
	log.G(ctx).WithField("id", t.ID()).Debug("pausing task")
	return t.unit.Stop(ctx)
}

// Resume unpauses the container process
func (t *LateInit) Resume(ctx context.Context) error {
	log.G(ctx).WithField("id", t.ID()).Debug("resume task")
	return t.unit.Start(ctx)
}

// Exec adds a process into the container
func (t *LateInit) Exec(ctx context.Context, id string, opts runtime.ExecOpts) (runtime.Process, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("adds a process into the task")
	s, err := t.getService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Exec(ctx, id, opts)
}

// Pids returns all pids
func (t *LateInit) Pids(ctx context.Context) ([]runtime.ProcessInfo, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("list all pids in container")
	s, err := t.getService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Pids(ctx)
}

// Checkpoint checkpoints a container to an image with live system data
func (t *LateInit) Checkpoint(ctx context.Context, path string, opts *types.Any) error {
	log.G(ctx).WithField("id", t.ID()).Debug("checkpoints task")
	s, err := t.getService(ctx)
	if err != nil {
		return err
	}
	return s.Checkpoint(ctx, path, opts)
}

// Update sets the provided resources to a running task
func (t *LateInit) Update(ctx context.Context, resources *types.Any, annotations map[string]string) error {
	log.G(ctx).WithField("id", t.ID()).Debug("update task")
	s, err := t.getService(ctx)
	if err != nil {
		return err
	}
	return s.Update(ctx, resources, annotations)
}

// Process returns a process within the task for the provided id
func (t *LateInit) Process(ctx context.Context, id string) (runtime.Process, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("get process from task")
	s, err := t.getService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Process(ctx, id)
}

// Stats returns runtime specific metrics for a task
func (t *LateInit) Stats(ctx context.Context) (*types.Any, error) {
	log.G(ctx).WithField("id", t.ID()).Debug("get stats from task")
	s, err := t.getService(ctx)
	if err != nil {
		return nil, err
	}
	return s.Stats(ctx)
}

func (t *LateInit) getService(ctx context.Context) (*TaskService, error) {
	if s, closed := t.holder.getService(); s != nil {
		return s, nil
	} else if closed {
		return nil, ErrTaskIsKilled
	}

	getService, closed := t.holder.getNextService()
	if closed {
		return nil, ErrTaskIsKilled
	}

	if s, err := getService(ctx); err != nil {
		return nil, err
	} else if s != nil {
		return s, nil
	}

	return nil, ErrTaskIsKilled
}

func (t *LateInit) setExited(exit *runtime.Exit) {
	t.Lock()
	defer t.Unlock()

	t.exited = exit
}

func (t *LateInit) getExited() *runtime.Exit {
	t.Lock()
	defer t.Unlock()

	return t.exited
}
