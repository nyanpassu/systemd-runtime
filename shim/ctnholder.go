package shim

import (
	"sync"

	"github.com/pkg/errors"

	"github.com/projecteru2/systemd-runtime/runc"
)

var (
	// ErrContainerDeleted .
	ErrContainerDeleted = errors.New("container deleted")
	// ErrContainerNotCreated .
	ErrContainerNotCreated = errors.New("container not created")
	// ErrIncorrectContainerID .
	ErrIncorrectContainerID = errors.New("incorrect container id")
)

type ContainerHolder struct {
	mu        sync.RWMutex
	container *runc.Container
	deleted   bool
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

func (h *ContainerHolder) GetLockedContainer() (_ *runc.Container, _ func(), err error) {
	h.mu.RLock()

	if h.container == nil {
		defer h.mu.RUnlock()
		if h.deleted {
			return nil, nil, ErrContainerDeleted
		}
		return nil, nil, ErrContainerNotCreated
	}

	return h.container, h.mu.RUnlock, nil
}

func (h *ContainerHolder) GetLockedContainerForDelete() (*runc.Container, func(), func(), error) {
	h.mu.Lock()

	if h.container == nil {
		defer h.mu.Unlock()
		if h.deleted {
			return nil, nil, nil, ErrContainerDeleted
		}
		return nil, nil, nil, ErrContainerNotCreated
	}

	return h.container, func() {
			defer h.mu.Unlock()

			h.deleted = true
			h.container = nil
		}, func() {
			defer h.mu.Unlock()
		}, nil
}
