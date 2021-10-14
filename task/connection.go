package task

import (
	"context"
	"sync"
	"time"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime"
	taskapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/projecteru2/systemd-runtime/common"
	"github.com/projecteru2/systemd-runtime/shim"
)

const (
	maxRetry     = 10
	waitInterval = time.Duration(120) * time.Second
	connTimeout  = time.Duration(10) * time.Second
	killTimeout  = time.Duration(10) * time.Second
)

type ConnMng struct {
	sync.Mutex
	logger      *logrus.Entry
	service     *TaskService
	bundle      common.Bundle
	exitStatus  *runtime.Exit
	subscribers []chan<- *TaskService
}

func (s *ConnMng) getService() *TaskService {
	s.Lock()
	defer s.Unlock()

	return s.service
}

// return error either task is paused or disabled
func (s *ConnMng) getRunningService(ctx context.Context) (*TaskService, error) {
	service := s.getService()
	if service != nil {
		return service, nil
	}

	status, err := s.bundle.Status(ctx)
	if err != nil {
		return nil, err
	}
	if status.Disabled {
		return nil, ErrTaskIsKilled
	}
	return nil, ErrTaskIsPaused
}

// wait service connection, if service is exited, return exit status
func (s *ConnMng) waitService(ctx context.Context) (*TaskService, *runtime.Exit, error) {
	ch := s.subscribe()

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case service := <-ch:
		if service != nil {
			return service, nil, nil
		}

		status, err := s.bundle.Status(ctx)
		if err != nil {
			return nil, nil, err
		}
		return nil, status.Exit, nil
	}
}

func (s *ConnMng) waitRunningService(ctx context.Context) (*TaskService, error) {
	service, exit, err := s.waitService(ctx)
	if err != nil {
		return nil, err
	}
	if exit != nil {
		return nil, ErrTaskIsKilled
	}
	return service, nil
}

// if force is true, then will not waiting for more connection to shim
func (s *ConnMng) exit(exit *runtime.Exit) {
	subscribers := func() []chan<- *TaskService {
		s.Lock()
		defer s.Unlock()

		s.exitStatus = exit
		subscribers := s.subscribers
		s.subscribers = nil

		return subscribers
	}()

	for _, sub := range subscribers {
		close(sub)
	}
}

func (s *ConnMng) subscribe() <-chan *TaskService {
	ch := make(chan *TaskService, 1)
	s.Lock()
	defer s.Unlock()

	s.subscribers = append(s.subscribers, ch)
	return ch
}

func (s *ConnMng) resetService() {
	s.Lock()
	defer s.Unlock()

	s.service = nil
}

func (s *ConnMng) setService(service *TaskService) {
	subscribers := func() []chan<- *TaskService {
		s.Lock()
		defer s.Unlock()

		s.service = service
		subscribers := s.subscribers
		s.subscribers = nil

		return subscribers
	}()

	for _, sub := range subscribers {
		sub <- service
		close(sub)
	}
}

func (s *ConnMng) connect() {
	retryCount := 0
	for {
		status, running, err := s.bundle.ShimStatus(context.Background())
		if err != nil {
			logrus.WithError(err).Error("check shim status error")
			retryCount++
		}
		if retryCount >= maxRetry {
			if err := s.bundle.Delete(context.Background()); err != nil {
				logrus.WithError(err).Error("delete bundle error")
			}
			return
		}
		if status.Disabled && !running {
			s.exit(status.Exit)
			return
		}
		if s.doConnect(waitInterval, connTimeout) {
			return
		}
	}
}

func (s *ConnMng) getAddr(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return common.ReceiveAddressOverFifo(ctx, s.bundle.Path())
}

func (s *ConnMng) connOnAddr(timeout time.Duration, addr string) error {
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
	taskPid, err := connect(ctx, taskClient, s.bundle.ID())
	if err != nil {
		return errors.WithMessage(err, "connect task service failed")
	}

	se := &TaskService{
		taskPid:     taskPid,
		id:          s.bundle.ID(),
		namespace:   s.bundle.Namespace(),
		taskService: taskClient,
	}
	s.setService(se)
	return nil
}

func (s *ConnMng) doConnect(waitInterval time.Duration, connTimeout time.Duration) bool {

	addr, err := s.getAddr(waitInterval)
	if err != nil {
		logrus.WithError(err).Error("receive address over fifo error")
		return false
	}

	if err := s.connOnAddr(connTimeout, addr); err != nil {
		s.logger.WithField(
			"address", addr,
		).WithError(
			err,
		).Error("connect on address error")
		return false
	}
	return true
}

func (s *ConnMng) reconnect() {
	s.resetService()

	s.logger.WithField("id", s.bundle.ID()).Debug("ttrpc conn disconnected, reconnect")
	go s.connect()
}
