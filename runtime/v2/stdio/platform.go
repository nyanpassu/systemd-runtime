package stdio

import (
	"context"
	"sync"

	"github.com/containerd/console"
	"github.com/containerd/containerd/errdefs"
	"github.com/pkg/errors"
)

type linuxPlatform struct {
	epoller *console.Epoller
}

// Platform handles platform-specific behavior that may differs across
// platform implementations
type Platform interface {
	CopyConsole(ctx context.Context, console console.Console, stdin, stdout, stderr string,
		wg *sync.WaitGroup) (console.Console, error)
	ShutdownConsole(ctx context.Context, console console.Console) error
	Close() error
}

// NewPlatform returns a linux platform for use with I/O operations
func NewPlatform() (Platform, error) {
	epoller, err := console.NewEpoller()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize epoller")
	}
	go epoller.Wait()
	return &linuxPlatform{
		epoller: epoller,
	}, nil
}

func (p *linuxPlatform) CopyConsole(ctx context.Context, console console.Console, stdin, stdout, stderr string, wg *sync.WaitGroup) (console.Console, error) {
	return nil, errdefs.ErrNotImplemented
	// if p.epoller == nil {
	// 	return nil, errors.New("uninitialized epoller")
	// }

	// epollConsole, err := p.epoller.Add(console)
	// if err != nil {
	// 	return nil, err
	// }

	// var cwg sync.WaitGroup
	// if stdin != "" {
	// 	in, err := fifo.OpenFifo(context.Background(), stdin, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	cwg.Add(1)
	// 	go func() {
	// 		cwg.Done()
	// 		bp := bufPool.Get().(*[]byte)
	// 		defer bufPool.Put(bp)
	// 		io.CopyBuffer(epollConsole, in, *bp)
	// 		// we need to shutdown epollConsole when pipe broken
	// 		epollConsole.Shutdown(p.epoller.CloseConsole)
	// 		epollConsole.Close()
	// 	}()
	// }

	// outw, err := fifo.OpenFifo(ctx, stdout, syscall.O_WRONLY, 0)
	// if err != nil {
	// 	return nil, err
	// }
	// outr, err := fifo.OpenFifo(ctx, stdout, syscall.O_RDONLY, 0)
	// if err != nil {
	// 	return nil, err
	// }
	// wg.Add(1)
	// cwg.Add(1)
	// go func() {
	// 	cwg.Done()
	// 	buf := bufPool.Get().(*[]byte)
	// 	defer bufPool.Put(buf)
	// 	io.CopyBuffer(outw, epollConsole, *buf)

	// 	outw.Close()
	// 	outr.Close()
	// 	wg.Done()
	// }()
	// cwg.Wait()
	// return epollConsole, nil
}

func (p *linuxPlatform) ShutdownConsole(ctx context.Context, cons console.Console) error {
	if p.epoller == nil {
		return errors.New("uninitialized epoller")
	}
	epollConsole, ok := cons.(*console.EpollConsole)
	if !ok {
		return errors.Errorf("expected EpollConsole, got %#v", cons)
	}
	return epollConsole.Shutdown(p.epoller.CloseConsole)
}

func (p *linuxPlatform) Close() error {
	return p.epoller.Close()
}
