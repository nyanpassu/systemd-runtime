package common

import (
	"github.com/pkg/errors"
)

var (
	ErrBundleDisabled           = errors.New("bundle disabled")
	ErrAquireFifoFileLockFailed = errors.New("aquire fifo file lock failed")
)
