package channel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/goware/logger"
)

type Channel[T any] interface {
	// ReadChannel returns the read-only message channel
	ReadChannel() <-chan T

	// SendChannel returns the send-only message channel
	SendChannel() chan<- T

	// Read message from the ReadChannel. A helper method in case you don't
	// want to use the ReadChannel directly. It will return (T, false) if
	// the read channel is closed.
	Read() (T, bool)

	// Send message to the SendChannel. Send is non-blocking and concurrent-safe.
	// A helper method in case you don't want to use the SendChannel directly. It will
	// return false if the send channel is closed instead of panicing.
	Send(message T) bool

	// Done channel to determine when the unbounded Channel has been closed.
	Done() <-chan struct{}

	// Close method will close the unbounded Channel and the send channel. Please
	// make sure to read all of the message from ReadChannel or call Flush() to
	// flush the channel so that the piping goroutine will exit.
	Close()

	// Flush will read any remaining buffered messages from the ReadChannel which
	// allows the ReadChannel to close. This is by design to allow slow consumers
	// to read messages from the ReadChannel even after the Channel has been closed.
	// However, we offer the Flush method to clean up after a close.
	Flush()
}

type Options struct {
	Logger  logger.Logger
	Alerter Alerter
}

type channel[T any] struct {
	id   uint64
	in   chan<- T
	out  chan T
	done chan struct{}
	mu   sync.RWMutex
}

var cid uint64 = 0

func NewUnboundedChan[T any](bufferLimitWarning, capacity int, options ...Options) Channel[T] {
	opts := Options{}
	if len(options) > 0 {
		opts = options[0]
	}

	in := make(chan T)  // send
	out := make(chan T) // read

	channel := &channel[T]{
		id:   atomic.AddUint64(&cid, 1),
		in:   in,
		out:  out,
		done: make(chan struct{}),
	}

	go func() {
		var queue []T

		for {
			if len(queue) == 0 {
				if message, ok := <-in; ok {
					if !(capacity <= 0) && len(queue) >= capacity {
						queue = queue[1:capacity] // truncate
					}
					queue = append(queue, message)
					if len(queue) > bufferLimitWarning {
						if opts.Logger != nil {
							opts.Logger.Warn(fmt.Sprintf("[send %d] channel queue holds %v > %v messages", channel.id, len(queue), bufferLimitWarning))
						}
						if opts.Alerter != nil {
							opts.Alerter.Alert(context.Background(), fmt.Sprintf("[send %d] channel queue limit of %v messages", channel.id, bufferLimitWarning))
						}
					}
				} else {
					close(out)
					return
				}
			} else {
				select {

				case out <- queue[0]:
					queue = queue[1:]

				case message, ok := <-in:
					if ok {
						if !(capacity <= 0) && len(queue) >= capacity {
							queue = queue[1:capacity] // truncate
						}
						queue = append(queue, message)
						if len(queue) > bufferLimitWarning {
							if opts.Logger != nil {
								opts.Logger.Warn(fmt.Sprintf("[read %d] channel queue holds %v > %v messages", channel.id, len(queue), bufferLimitWarning))
							}
							if opts.Alerter != nil {
								opts.Alerter.Alert(context.Background(), fmt.Sprintf("[read %d] channel queue limit of %v messages", channel.id, bufferLimitWarning))
							}
						}
					}
				}
			}
		}
	}()

	return channel
}

func (c *channel[T]) Done() <-chan struct{} {
	return c.done
}

func (c *channel[T]) ReadChannel() <-chan T {
	return c.out
}

func (c *channel[T]) SendChannel() chan<- T {
	return c.in
}

func (c *channel[T]) Read() (T, bool) {
	select {
	case <-c.done:
		var v T
		return v, false
	case v, ok := <-c.out:
		return v, ok
	}
}

func (c *channel[T]) Send(message T) bool {
	select {
	case <-c.done:
		return false
	default:
		c.mu.RLock()
		c.in <- message
		c.mu.RUnlock()
		return true
	}
}

func (c *channel[T]) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
		c.mu.Lock()
		close(c.in)
		c.mu.Unlock()
	}
}

func (c *channel[T]) Flush() {
	select {
	case <-c.done:
		for range c.out {
		}
	default:
	}
}
