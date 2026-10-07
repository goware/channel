package channel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

type Channel[T any] interface {
	// ReadChannel returns the read-only message channel
	ReadChannel() <-chan T

	// SendChannel returns the send-only message channel. Sending on it directly
	// panics if the Channel is closed concurrently; Send does not.
	SendChannel() chan<- T

	// Read message from the ReadChannel. A helper method in case you don't
	// want to use the ReadChannel directly. It will return (T, false) if
	// the read channel is closed.
	Read() (T, bool)

	// Send queues a message and is safe to call concurrently with Close.
	// It waits for the queueing goroutine to accept the message, which can
	// include waiting for synchronous logging or alerts. It returns false
	// if the channel is closed.
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
	Logger  *slog.Logger
	Alerter Alerter
	Label   string
}

type channel[T any] struct {
	id    uint64
	label string // optional
	in    chan<- T
	out   chan T
	done  chan struct{}
	mu    sync.RWMutex
}

var cid uint64 = 0

// NewUnboundedChan returns a Channel that queues messages until they are read.
// It warns through the Logger and Alerter when more than bufferLimitWarning
// messages are queued, and again each time the queue doubles. When capacity
// is above 0, the oldest message is dropped to make room once capacity
// messages are queued.
func NewUnboundedChan[T any](bufferLimitWarning, capacity int, options ...Options) Channel[T] {
	opts := Options{}
	if len(options) > 0 {
		opts = options[0]
	}

	in := make(chan T)  // send
	out := make(chan T) // read

	channel := &channel[T]{
		id:    atomic.AddUint64(&cid, 1),
		label: opts.Label,
		in:    in,
		out:   out,
		done:  make(chan struct{}),
	}

	var label string
	if channel.label != "" {
		label = fmt.Sprintf("%d:%s", channel.id, channel.label)
	} else {
		label = fmt.Sprintf("%d", channel.id)
	}

	warn := func(logMsg string, alertFormat string, alertArgs ...interface{}) {
		if opts.Logger != nil {
			opts.Logger.Warn(logMsg)
		}
		if opts.Alerter != nil {
			opts.Alerter.Alert(context.Background(), alertFormat, alertArgs...)
		}
	}

	go func() {
		var queue []T
		recv := (<-chan T)(in) // nil once in is closed

		// The warning fires when the queue passes bufferLimitWarning and again
		// each time it doubles, and the capacity alert fires on the first drop.
		// Draining the queue re-arms both.
		warnAbove := bufferLimitWarning
		dropping := false

		push := func(message T) {
			if capacity > 0 && len(queue) >= capacity {
				if !dropping {
					dropping = true
					const format = "[send %s] channel queue is at capacity of %v messages, dropping the oldest"
					warn(fmt.Sprintf(format, label, capacity), format, label, capacity)
				}
				var zero T
				queue[0] = zero // let the dropped message be collected
				queue = queue[1:]
			}
			queue = append(queue, message)
			if len(queue) > warnAbove {
				warnAbove = len(queue) * 2
				warn(
					fmt.Sprintf("[send %s] channel queue holds %v > %v messages", label, len(queue), bufferLimitWarning),
					"[send %s] channel queue limit of %v messages", label, bufferLimitWarning,
				)
			}
		}

		for {
			if len(queue) == 0 {
				if recv == nil {
					break
				}
				message, ok := <-recv
				if !ok {
					break
				}
				push(message)
				continue
			}

			select {
			case out <- queue[0]:
				var zero T
				queue[0] = zero // let the delivered message be collected
				queue = queue[1:]
				if len(queue) == 0 {
					queue = nil // release the backing array after a burst
					warnAbove = bufferLimitWarning
					dropping = false
				}

			case message, ok := <-recv:
				if !ok {
					// Closed: stop receiving, rather than spinning on the
					// closed channel until readers drain the queue.
					recv = nil
					continue
				}
				push(message)
			}
		}
		close(out)
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
	// Hold the read lock across the closed check and the send, so Close cannot
	// close the send channel in between.
	c.mu.RLock()
	defer c.mu.RUnlock()
	select {
	case <-c.done:
		return false
	default:
	}
	c.in <- message
	return true
}

func (c *channel[T]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
	default:
		close(c.done)
		close(c.in)
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
