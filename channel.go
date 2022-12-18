package channel

import (
	"github.com/goware/logger"
)

type Channel[T any] interface {
	Read() (T, bool)
	Send(message T) bool
	ReadChannel() <-chan T
	SendChannel() chan<- T
	Done() <-chan struct{}
	Close()
}

type channel[T any] struct {
	readCh chan T
	sendCh chan<- T
	done   chan struct{}

	log                logger.Logger
	bufferLimitWarning int
}

func NewUnboundedChan[T any](log logger.Logger, bufferLimitWarning int) Channel[T] {
	readCh := make(chan T)
	sendCh := make(chan T)

	channel := &channel[T]{
		readCh:             readCh,
		sendCh:             sendCh,
		done:               make(chan struct{}),
		log:                log,
		bufferLimitWarning: bufferLimitWarning,
	}

	go func() {
		var buffer []T

		for {
			if len(buffer) == 0 {
				if message, ok := <-sendCh; ok {
					buffer = append(buffer, message)
					if len(buffer) > bufferLimitWarning {
						log.Warnf("channel buffer holds %v > %v messages", len(buffer), bufferLimitWarning)
					}
				} else {
					close(readCh)
					return
				}
			} else {
				select {
				case <-channel.done:
					continue

				case readCh <- buffer[0]:
					buffer = buffer[1:]

				case message, ok := <-sendCh:
					if ok {
						buffer = append(buffer, message)
						if len(buffer) > bufferLimitWarning {
							log.Warnf("channel buffer holds %v > %v messages", len(buffer), bufferLimitWarning)
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
	return c.readCh
}

func (c *channel[T]) SendChannel() chan<- T {
	return c.sendCh
}

func (c *channel[T]) Read() (T, bool) {
	select {
	case <-c.done:
		var v T
		return v, false
	case v, ok := <-c.readCh:
		return v, ok
	}
}

func (c *channel[T]) Send(message T) bool {
	select {
	case <-c.done:
		return false
	default:
		c.sendCh <- message
		return true
	}
}

func (c *channel[T]) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
		close(c.sendCh)
	}
}
