package imgproc

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"time"
)

// ErrBusy means image work could not get a slot in time. It is not a verdict
// on the file: callers answer that the server is busy and leave stored files
// alone.
var ErrBusy = errors.New("the server is busy, try again shortly")

// slots bounds how many pictures are decoded and redrawn at once, so a burst
// of animated GIFs cannot spend the CPU the voice forwarder needs. Same shape
// as the bcrypt slots.
var slots = make(chan struct{}, max(1, runtime.GOMAXPROCS(0)))

var (
	waiting  atomic.Int32
	maxQueue = int32(4 * cap(slots))
	maxWait  = 2 * time.Second
)

// Gate holds one image-processing slot until the returned function runs.
// It gives up when ctx ends, after maxWait, or at once when the queue is
// already full.
func Gate(ctx context.Context) (func(), error) {
	release := func() { <-slots }
	select {
	case slots <- struct{}{}:
		return release, nil
	default:
	}
	if waiting.Add(1) > maxQueue {
		waiting.Add(-1)
		return nil, ErrBusy
	}
	defer waiting.Add(-1)
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	select {
	case slots <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrBusy
	}
}

func acquire() (func(), error) {
	return Gate(context.Background())
}
