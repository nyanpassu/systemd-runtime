package shim

import (
	"context"
	"sync"

	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/runc"

	"github.com/sirupsen/logrus"

	"github.com/projecteru2/systemd-runtime/utils"
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

func (s *EventSender) SendEventContainerExit(evt eventstypes.TaskExit, status *SyncedServiceStatus) {
	// if service is killed by shimservice api, then we send the exit event
	if status.oneOf(ServiceKilling, ServiceKilled) {
		s.SendL(evt)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.containerExitEvt = evt
}

func (s *EventSender) Send(evt interface{}) {
	s.events <- evt
}

func (s *EventSender) PrepareSendL() (func(interface{}), func()) {
	s.mu.Lock()

	once := utils.NewOnce(func() {
		s.mu.Unlock()
	})

	return func(evt interface{}) {

			s.events <- evt
		}, func() {
			once.Run()
		}
}

func (s *EventSender) SendL(evt interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events <- evt
}
