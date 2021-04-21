package events

import (
	"github.com/containerd/containerd/api/events"
	"github.com/sirupsen/logrus"
)

const (
	// TaskCreateEventTopic for task create
	TaskCreateEventTopic = "/tasks/create"
	// TaskStartEventTopic for task start
	TaskStartEventTopic = "/tasks/start"
	// TaskOOMEventTopic for task oom
	TaskOOMEventTopic = "/tasks/oom"
	// TaskExitEventTopic for task exit
	TaskExitEventTopic = "/tasks/exit"
	// TaskDeleteEventTopic for task delete
	TaskDeleteEventTopic = "/tasks/delete"
	// TaskExecAddedEventTopic for task exec create
	TaskExecAddedEventTopic = "/tasks/exec-added"
	// TaskExecStartedEventTopic for task exec start
	TaskExecStartedEventTopic = "/tasks/exec-started"
	// TaskPausedEventTopic for task pause
	TaskPausedEventTopic = "/tasks/paused"
	// TaskResumedEventTopic for task resume
	TaskResumedEventTopic = "/tasks/resumed"
	// TaskCheckpointedEventTopic for task checkpoint
	TaskCheckpointedEventTopic = "/tasks/checkpointed"
	// TaskUnknownTopic for unknown task events
	TaskUnknownTopic = "/tasks/?"
)

// GetTopic converts an event from an interface type to the specific
// event topic id
func GetTopic(e interface{}) string {
	switch e.(type) {
	case *events.TaskCreate:
		return TaskCreateEventTopic
	case *events.TaskStart:
		return TaskStartEventTopic
	case *events.TaskOOM:
		return TaskOOMEventTopic
	case *events.TaskExit:
		return TaskExitEventTopic
	case *events.TaskDelete:
		return TaskDeleteEventTopic
	case *events.TaskExecAdded:
		return TaskExecAddedEventTopic
	case *events.TaskExecStarted:
		return TaskExecStartedEventTopic
	case *events.TaskPaused:
		return TaskPausedEventTopic
	case *events.TaskResumed:
		return TaskResumedEventTopic
	case *events.TaskCheckpointed:
		return TaskCheckpointedEventTopic
	default:
		logrus.Warnf("no topic for type %#v", e)
	}
	return TaskUnknownTopic
}
