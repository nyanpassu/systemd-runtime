package systemd

import (
	"context"
	"os/exec"
)

type Unit struct {
	Name    string
	manager *UnitManager
}

func (u *Unit) Enable(ctx context.Context) error {
	return cmd("enable", u.Name)
}

func (u *Unit) Disable(ctx context.Context) error {
	return cmd("disable", u.Name)
}

func (u *Unit) Start(ctx context.Context) error {
	return cmd("start", u.Name)
}

func (u *Unit) Stop(ctx context.Context) error {
	return cmd("stop", u.Name)
}

func (u *Unit) Remove(ctx context.Context) error {
	return u.manager.Remove(ctx, u.Name)
}

func cmd(args ...string) error {
	return exec.Command("systemctl", args...).Run()
}
