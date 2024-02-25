package channel_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/goware/channel"
	"github.com/goware/logger"
)

func TestSlowProducer(t *testing.T) {
	testUnboundedBufferedChannel(t, 100*time.Millisecond, 0, 20)
}

func TestSlowConsumer(t *testing.T) {
	testUnboundedBufferedChannel(t, 0, 10*time.Millisecond, 100)
}

func TestClosed(t *testing.T) {
	ch := channel.NewUnboundedChan[int](10, 1000, channel.Options{Logger: logger.NewLogger(logger.LogLevel_INFO)})

	go func() {
		ch.Send(1)
		ch.Close()
		ch.Flush()
	}()

	time.Sleep(1 * time.Second)

	ok := ch.Send(2)
	ok = ch.Send(2)
	ok = ch.Send(2)
	ok = ch.Send(2)
	ch.Flush()
	fmt.Println("ok?", ok)
}

func TestCapacity(t *testing.T) {
	ch := channel.NewUnboundedChan[int](10, 20, channel.Options{Logger: logger.NewLogger(logger.LogLevel_INFO)})

	go func() {
		for i := 0; i < 40; i++ {
			ch.Send(i)
		}
		ch.Close()
	}()

	time.Sleep(1 * time.Second)

	for msg := range ch.ReadChannel() {
		fmt.Println("=> msg", msg)
	}
}

func testUnboundedBufferedChannel(t *testing.T, producerDelay time.Duration, consumerDelay time.Duration, messages int) {
	ch := channel.NewUnboundedChan[string](5, 1000, channel.Options{Logger: logger.NewLogger(logger.LogLevel_INFO)})

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		expected := 0
		for msg, ok := <-ch.ReadChannel(); ok; msg, ok = <-ch.ReadChannel() {
			fmt.Printf("received message %v\n", msg)
			time.Sleep(consumerDelay)
			if msg != fmt.Sprintf("-> msg:%d", expected) {
				t.Logf("expected '%s'", msg)
				t.Fail()
			}
			expected++
		}

		if messages != expected {
			t.Logf("expected '%d'", messages)
			t.Fail()
		}
		wg.Done()
	}()

	for i := 0; i < messages; i++ {
		time.Sleep(producerDelay)
		fmt.Printf("sending message %v\n", i)
		// ch.SendChannel() <- fmt.Sprintf("-> msg:%d", i)
		ch.Send(fmt.Sprintf("-> msg:%d", i))
	}

	ch.Close()
	wg.Wait()
}

func TestChaos(t *testing.T) {
	// attempting to test if we can get Send in a blocking after having
	// closed the channel

	ch := channel.NewUnboundedChan[int](100, 100, channel.Options{Logger: logger.NewLogger(logger.LogLevel_INFO)})

	// writer
	go func() {
		for i := 0; i < 100; i++ {
			ch.Send(i)
			// time.Sleep(10 * time.Millisecond)
		}
	}()

	// reader
	go func() {
		for i := 0; i < 10; i++ {
			msg, ok := ch.Read()
			if !ok {
				fmt.Println("read done.")
				return
			}
			fmt.Println("msg", msg)
		}
		ch.Close()
		ch.Flush()
	}()

	time.Sleep(1 * time.Second)
}
