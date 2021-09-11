package shim

import "sync"

type ServiceStatus int

const (
	ServiceCreated = iota
	ServiceStarted
	ServiceKilling
	ServiceKilled
)

type SyncedServiceStatus struct {
	mu     sync.Mutex
	status ServiceStatus
}

func (s *SyncedServiceStatus) SetStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == ServiceCreated {
		s.status = ServiceStarted
	}
}

func (s *SyncedServiceStatus) HasStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status != ServiceCreated
}

func (s *SyncedServiceStatus) Kill() (func(), func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.status
	s.status = ServiceKilling

	return func() {
			s.mu.Lock()
			defer s.mu.Unlock()

			s.status = ServiceKilled
		}, func() {
			s.mu.Lock()
			defer s.mu.Unlock()

			s.status = prev
		}
}

func (s *SyncedServiceStatus) oneOf(status ServiceStatus, statuses ...ServiceStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == status {
		return true
	}

	for _, st := range statuses {
		if s.status == st {
			return true
		}
	}

	return false
}
