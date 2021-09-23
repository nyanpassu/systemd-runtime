package task

import (
	"context"
	"sync"
	"time"

	"github.com/containerd/containerd/log"
	taskapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/shim"
	"github.com/projecteru2/systemd-runtime/utils"
)

const waitInterval = time.Duration(120) * time.Second
const connTimeout = time.Duration(10) * time.Second
const killTimeout = time.Duration(10) * time.Second

type kill struct {
	signal uint32
	all    bool
}

type ServiceHolder struct {
	sync.Mutex
	service     *TaskService
	closed      bool
	subscribers []chan *TaskService
}

func (h *ServiceHolder) SetService(s *TaskService) {
	h.Lock()
	defer h.Unlock()

	h.service = s
	if len(h.subscribers) != 0 {
		subscribers := h.subscribers
		h.subscribers = nil
		go func() {
			for _, subscriber := range subscribers {
				subscriber <- s
			}
		}()
	}
}

func (h *ServiceHolder) Close() {
	h.Lock()
	defer h.Unlock()

	h.service = nil
	h.closed = true
	if len(h.subscribers) != 0 {
		subscribers := h.subscribers
		h.subscribers = nil
		go func() {
			for _, subscriber := range subscribers {
				close(subscriber)
			}
		}()
	}
}

func (h *ServiceHolder) Closed() bool {
	h.Lock()
	defer h.Unlock()

	return h.closed
}

func (h *ServiceHolder) getService() (*TaskService, bool) {
	h.Lock()
	defer h.Unlock()

	return h.service, h.closed
}

func (h *ServiceHolder) getNextService() (
	func(context.Context) (*TaskService, error), bool,
) {
	h.Lock()
	defer h.Unlock()

	if h.service != nil {
		s := h.service
		return func(context.Context) (*TaskService, error) {
			return s, nil
		}, true
	}

	if h.closed {
		return nil, false
	}

	ch := make(chan *TaskService, 1)
	h.subscribers = append(h.subscribers, ch)
	return func(ctx context.Context) (*TaskService, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case s := <-ch:
			return s, nil
		}
	}, true
}

type ConnMng struct {
	id        string
	namespace string
	bundle    common.Bundle

	sync.Mutex
	cancel  *utils.Once
	killSig *kill
	holder  *ServiceHolder
}

func (s *ConnMng) kill(signal uint32, all bool) {
	s.Lock()
	defer s.Unlock()

	s.killSig = &kill{
		signal: signal,
		all:    all,
	}
}

func (s *ConnMng) killNow(signal uint32, all bool) {
	s.Lock()
	defer s.Unlock()

	s.killSig = &kill{
		signal: signal,
		all:    all,
	}
	if s.cancel != nil {
		s.cancel.Run()
	}
}

func (s *ConnMng) killed() bool {
	s.Lock()
	defer s.Unlock()

	return s.killSig != nil
}

func (s *ConnMng) connect() {
	for {
		disabled, running, err := s.bundle.Disabled(context.Background())
		if err == nil {
			if disabled && !running {
				s.holder.Close()
				return
			}
		}

		if s.doConnect(waitInterval, connTimeout) {
			return
		}
		if s.killed() {
			s.holder.Close()
			return
		}
	}
}

func (s *ConnMng) getAddr(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	c := s.setCancel(cancel)
	defer c.Run()

	return common.ReceiveAddressOverFifo(ctx, s.bundle.Path())
}

func (s *ConnMng) setCancel(cancel func()) *utils.Once {
	s.Lock()
	defer s.Unlock()

	c := utils.NewOnce(cancel)
	s.cancel = c
	return c
}

func (s *ConnMng) connOnAddr(timeout time.Duration, addr string) error {
	s.Lock()
	defer s.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.G(ctx).Debug("create shim connection")
	conn, err := shim.Connect(addr, shim.AnonReconnectDialer)
	if err != nil {
		return errors.WithMessage(err, "make shim connection failed")
	}

	client := ttrpc.NewClient(conn, ttrpc.WithOnClose(s.reconnect))
	taskClient := taskapi.NewTaskClient(client)

	log.G(ctx).Debug("connect shim service")
	taskPid, err := connect(ctx, taskClient, s.id)
	if err != nil {
		return errors.WithMessage(err, "connect task service failed")
	}

	se := &TaskService{
		taskPid:     taskPid,
		id:          s.id,
		namespace:   s.namespace,
		taskService: taskClient,
	}
	if s.killSig != nil {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), killTimeout)
			defer cancel()

			if err := se.Kill(ctx, s.killSig.signal, s.killSig.all); err != nil {
				log.G(ctx).WithField(
					"id", s.id,
				).WithField(
					"namespace", s.namespace,
				).WithError(
					err,
				).Error("kill task error")
			}
		}()
		return nil
	}

	s.holder.SetService(se)
	return nil
}

func (s *ConnMng) doConnect(waitInterval time.Duration, connTimeout time.Duration) bool {
	log.G(context.TODO()).Debug("receive address over fifo")

	addr, err := s.getAddr(waitInterval)
	if err != nil {
		log.G(context.TODO()).WithError(err).Error("receive address over fifo error")
		return false
	}

	if err := s.connOnAddr(connTimeout, addr); err != nil {
		log.G(context.TODO()).WithField(
			"address", addr,
		).WithError(
			err,
		).Error(
			"connect on address error",
		)
		return false
	}
	return true
}

func (s *ConnMng) reconnect() {
	s.Lock()
	defer s.Unlock()

	if s.killSig != nil {
		s.holder.Close()
		return
	}
	s.holder.SetService(nil)

	log.G(context.TODO()).WithField("id", s.id).Debug("ttrpc conn disconnected, reconnect")
	// must perform async connect
	go s.connect()
}
