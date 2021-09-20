package shim

import (
	"context"

	"github.com/containerd/containerd/errdefs"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	types1 "github.com/gogo/protobuf/types"
)

// Init func for the creation of a shim server
// type Init func(context.Context, string, Publisher, func()) (Shim, error)

type ShimService interface {
	Start(ctx context.Context, req *shimapi.StartRequest) (*shimapi.StartResponse, error)
	State(ctx context.Context, req *shimapi.StateRequest) (*shimapi.StateResponse, error)
	Delete(ctx context.Context, req *shimapi.DeleteRequest) (*shimapi.DeleteResponse, error)
	Pids(ctx context.Context, req *shimapi.PidsRequest) (*shimapi.PidsResponse, error)
	Pause(ctx context.Context, req *shimapi.PauseRequest) (*types1.Empty, error)
	Resume(ctx context.Context, req *shimapi.ResumeRequest) (*types1.Empty, error)
	Checkpoint(ctx context.Context, req *shimapi.CheckpointTaskRequest) (*types1.Empty, error)
	Kill(ctx context.Context, req *shimapi.KillRequest) (*types1.Empty, error)
	Exec(ctx context.Context, req *shimapi.ExecProcessRequest) (*types1.Empty, error)
	ResizePty(ctx context.Context, req *shimapi.ResizePtyRequest) (*types1.Empty, error)
	CloseIO(ctx context.Context, req *shimapi.CloseIORequest) (*types1.Empty, error)
	Update(ctx context.Context, req *shimapi.UpdateTaskRequest) (*types1.Empty, error)
	Wait(ctx context.Context, req *shimapi.WaitRequest) (*shimapi.WaitResponse, error)
	Stats(ctx context.Context, req *shimapi.StatsRequest) (*shimapi.StatsResponse, error)
	Connect(ctx context.Context, req *shimapi.ConnectRequest) (*shimapi.ConnectResponse, error)
	Shutdown(ctx context.Context, req *shimapi.ShutdownRequest) (*types1.Empty, error)
}

type TaskService struct {
	ShimService
}

func (t TaskService) Create(ctx context.Context, req *shimapi.CreateTaskRequest) (*shimapi.CreateTaskResponse, error) {
	return nil, errdefs.ErrNotImplemented
}
