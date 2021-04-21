package main

import (
	"context"
	"net"

	"google.golang.org/grpc"

	ctr "github.com/projecteru2/systemd-runtime/ctr/v1"
)

type dialResult struct {
	net.Conn
	error
}

func client(path string) (ctr.CtrClient, func() error, error) {
	conn, err := grpc.Dial(
		path,
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, path string) (net.Conn, error) {
			ch := make(chan dialResult, 1)
			go func() {
				conn, err := net.Dial("unix", path)
				ch <- dialResult{conn, err}
				close(ch)
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-ch:
				return r.Conn, r.error
			}
		}),
	)
	if err != nil {
		return nil, nil, err
	}

	return ctr.NewCtrClient(conn), func() error {
		return conn.Close()
	}, nil
}
