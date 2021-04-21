package utils

import (
	"context"
	"time"
)

var timeout time.Duration

var fakeCancel func() = func() {}

// Context .
func Context() (context.Context, func()) {
	if timeout == 0 {
		return context.Background(), fakeCancel
	}
	return context.WithTimeout(context.Background(), timeout)
}
