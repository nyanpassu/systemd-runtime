package shim

import (
	"context"
	"sync"

	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/runc"

	"github.com/sirupsen/logrus"
)

type EventSender struct {
	mu               sync.Mutex
	events           chan interface{}
	containerExitEvt interface{}
}

func NewEventSender() *EventSender {
	return &EventSender{
		events: make(chan interface{}, 128),
	}
}

func (s *EventSender) SetPublisher(ctx context.Context, publisher Publisher) {
	go func() {
		ns, _ := namespaces.Namespace(ctx)
		ctx = namespaces.WithNamespace(context.Background(), ns)
		for e := range s.events {
			err := publisher.Publish(ctx, runc.GetTopic(e), e)
			if err != nil {
				logrus.WithError(err).Error("post event")
			}
		}
		publisher.Close()
	}()
}

func (s *EventSender) Close() {
	close(s.events)
}

func (s *EventSender) SendEventContainerExit(evt *eventstypes.TaskExit, status *SyncedServiceStatus) {
	// if service is killed by shimservice api, then we send the exit event
	if status.oneOf(ServiceKilling, ServiceKilled) {
		s.SendEventExit(evt)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.containerExitEvt = evt
}

func (s *EventSender) SendEventCreate(evt *eventstypes.TaskCreate) {
	s.send(evt)
}

func (s *EventSender) SendEventOOM(evt *eventstypes.TaskOOM) {
	s.send(evt)
}

func (s *EventSender) SendEventDelete(evt *eventstypes.TaskDelete) {
	s.send(evt)
}

func (s *EventSender) SendEventExecAdded(evt *eventstypes.TaskExecAdded) {
	s.send(evt)
}

func (s *EventSender) SendEventPaused(evt *eventstypes.TaskPaused) {
	s.send(evt)
}

func (s *EventSender) SendEventResumed(evt *eventstypes.TaskResumed) {
	s.send(evt)
}

func (s *EventSender) SendEventCheckpointed(evt *eventstypes.TaskCheckpointed) {
	s.send(evt)
}

func (s *EventSender) send(evt interface{}) {
	s.events <- evt
}

func (s *EventSender) PrepareSendL() *EventStartSender {
	s.mu.Lock()

	return &EventStartSender{
		s: s,
	}
}

func (s *EventSender) SendEventExit(evt *eventstypes.TaskExit) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events <- evt
}

type EventStartSender struct {
	s *EventSender
}

func (s *EventStartSender) SendTaskStart(evt *eventstypes.TaskStart) {
	s.s.send(evt)
}

func (s *EventStartSender) SendExecStart(evt *eventstypes.TaskExecStarted) {
	s.s.send(evt)
}

func (s *EventStartSender) Cancel() {
	s.s.mu.Unlock()
}
