package common

import (
	"github.com/pkg/errors"
)

var (
	// ErrFileNotLocked .
	ErrFileNotLocked = errors.New("file not locked")
	// ErrBundleDisabled .
	ErrBundleDisabled = errors.New("bundle disabled")
	// ErrBundleIsAlreadyRunning .
	ErrBundleIsAlreadyRunning = errors.New("bundle is already running")
)
