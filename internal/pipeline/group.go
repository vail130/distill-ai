package pipeline

import (
	"context"
	"sync"
)

// group is a tiny errgroup-equivalent inlined to avoid adding the
// x/sync dependency for one type. It tracks N goroutines, captures
// the first error any of them returns, and cancels the supplied
// CancelFunc when an error occurs so peer goroutines can observe
// ctx.Done() and unwind.
//
// Replace with golang.org/x/sync/errgroup when a second use case
// justifies the dependency.
type group struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
	err    error
}

func newGroup(cancel context.CancelFunc) *group {
	return &group{cancel: cancel}
}

// Go runs f in a new goroutine. The first non-nil error any Go'd
// function returns is captured and the group's cancel is invoked.
func (g *group) Go(f func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := f(); err != nil {
			g.once.Do(func() {
				g.err = err
				if g.cancel != nil {
					g.cancel()
				}
			})
		}
	}()
}

// Wait blocks until every Go'd function has returned, then returns
// the first captured error (or nil).
func (g *group) Wait() error {
	g.wg.Wait()
	return g.err
}
