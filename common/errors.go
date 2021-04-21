package common

import (
	"github.com/pkg/errors"
)

var (
	ErrFileNotLocked          = errors.New("file not locked")
	ErrBundleDisabled         = errors.New("bundle disabled")
	ErrBundleIsAlreadyRunning = errors.New("bundle is already running")
)
