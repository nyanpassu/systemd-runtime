package systemd

import (
	"github.com/pkg/errors"
)

// ErrUnitExists .
var ErrUnitExists = errors.New("systemd unit exists")

// ErrUnitNotExists .
var ErrUnitNotExists = errors.New("systemd unit not exists")
