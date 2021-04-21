package systemd

import (
	"github.com/pkg/errors"
)

var ErrUnitExists = errors.New("systemd unit exists")
var ErrUnitNotExists = errors.New("systemd unit not exists")
