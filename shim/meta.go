package shim

import (
	"context"
)

type Meta struct {
	CreatedEventSent bool
}

func (m *Meta) Load(ctx context.Context) error {

}

func (m *Meta) Store(ctx context.Context) error {

}
