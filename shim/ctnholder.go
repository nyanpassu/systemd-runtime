package shim

import (
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/runtime/v2/runc"
)

var (
	ErrContainerDeleted     = errors.New("container deleted")
	ErrContainerNotCreated  = errors.New("container not created")
	ErrIncorrectContainerID = errors.New("incorrect container id")
)

type ContainerHolder struct {
	mu        sync.RWMutex
	container *runc.Container
	deleted   bool
}

type GetContainerOption struct {
	ID string
}

func (h *ContainerHolder) NewContainer(supplier func() (*runc.Container, error)) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	container, err := supplier()
	if err != nil {
		return 0, err
	}
	h.container = container
	return container.Pid(), nil
}

func (h *ContainerHolder) IsDeleted() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.deleted
}

func (h *ContainerHolder) GetLockedContainer(opt GetContainerOption) (_ *runc.Container, _ func(), err error) {
	locker := h.mu.RLocker()
	locker.Lock()

	if opt.ID != "" {
		if err := h.checkContainer(opt.ID); err != nil {
			locker.Unlock()
			return nil, nil, err
		}
	}

	return h.container, func() {
		locker.Unlock()
	}, nil
}

func (h *ContainerHolder) GetLockedContainerForDelete(opt GetContainerOption) (*runc.Container, func(), func(), error) {
	h.mu.Lock()

	if h.container == nil {
		defer h.mu.Unlock()
		if h.deleted {
			return nil, nil, nil, ErrContainerDeleted
		}
		return nil, nil, nil, ErrContainerNotCreated
	}

	if opt.ID != "" {
		if err := h.checkContainer(opt.ID); err != nil {
			h.mu.Unlock()
			return nil, nil, nil, err
		}
	}

	return h.container, func() {
			defer h.mu.Unlock()

			h.deleted = true
			h.container = nil
		}, func() {
			defer h.mu.Unlock()
		}, nil
}

func (h *ContainerHolder) checkContainer(id string) error {
	if h.deleted {
		return ErrContainerDeleted
	}

	if h.container == nil {
		return ErrContainerNotCreated
	}

	if id != "" && h.container.ID != id {
		logrus.WithField("id", id).Error("incorrect container id")
		return ErrIncorrectContainerID
	}
	return nil
}
