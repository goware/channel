package channel_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/goware/channel"
	"github.com/goware/logger"
	"github.com/stretchr/testify/assert"
)

func TestSlowProducer(t *testing.T) {
	testUnboundedBufferedChannel(t, 100*time.Millisecond, 0, 10)
}

func TestSlowConsumer(t *testing.T) {
	testUnboundedBufferedChannel(t, 0, 100*time.Microsecond, 100)
}

func TestClosed(t *testing.T) {
	ch := channel.NewUnboundedChan[int](logger.NewLogger(logger.LogLevel_INFO), 100)

	go func() {
		ch.Send(1)
		ch.Close()
	}()

	time.Sleep(1 * time.Second)

	ok := ch.Send(2)
	ok = ch.Send(2)
	ok = ch.Send(2)
	ok = ch.Send(2)
	fmt.Println("ok?", ok)
}

func testUnboundedBufferedChannel(t *testing.T, producerDelay time.Duration, consumerDelay time.Duration, messages int) {
	ch := channel.NewUnboundedChan[string](logger.NewLogger(logger.LogLevel_INFO), 100)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		expected := 0
		for msg, ok := <-ch.ReadChannel(); ok; msg, ok = <-ch.ReadChannel() {
			fmt.Printf("received message %v\n", msg)
			time.Sleep(consumerDelay)
			assert.Equal(t, fmt.Sprintf("-> msg:%d", expected), msg)
			expected++
		}

		assert.Equal(t, messages, expected)
		wg.Done()
	}()

	for i := 0; i < messages; i++ {
		fmt.Printf("sending message %v\n", i)
		// ch.SendChannel() <- fmt.Sprintf("-> msg:%d", i)
		ch.Send(fmt.Sprintf("-> msg:%d", i))
		time.Sleep(producerDelay)
	}

	ch.Close()
	wg.Wait()
}
