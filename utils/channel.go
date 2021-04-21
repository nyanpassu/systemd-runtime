package utils

import (
	"sync"
)

type Once struct {
	run   func()
	done  bool
	mutex sync.Mutex
}

func NewOnce(run func()) *Once {
	return &Once{
		run: run,
	}
}

func (once *Once) Run() {
	once.mutex.Lock()
	defer once.mutex.Unlock()

	if once.done {
		return
	}

	once.run()
}
