package task

import (
	"context"

	"github.com/containerd/containerd/runtime"

	taskapi "github.com/containerd/containerd/runtime/v2/task"
)

func StatusIntoString(s runtime.Status) string {
	switch s {
	case runtime.CreatedStatus:
		return "CreatedStatus"
	case runtime.RunningStatus:
		return "RunningStatus"
	case runtime.StoppedStatus:
		return "StoppedStatus"
	case runtime.DeletedStatus:
		return "DeletedStatus"
	case runtime.PausedStatus:
		return "PausedStatus"
	case runtime.PausingStatus:
		return "PausingStatus"
	default:
		return "UnknownStatus"
	}
}

func connect(ctx context.Context, taskService taskapi.TaskService, id string) (uint32, error) {
	response, err := taskService.Connect(ctx, &taskapi.ConnectRequest{
		ID: id,
	})
	if err != nil {
		return 0, err
	}
	return response.TaskPid, nil
}
