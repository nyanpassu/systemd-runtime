package common

import (
	"github.com/pkg/errors"
)

var (
	ErrFileNotLocked            = errors.New("file not locked")
	ErrBundleDisabled           = errors.New("bundle disabled")
	ErrAquireFifoFileLockFailed = errors.New("aquire fifo file lock failed")
)
