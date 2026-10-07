package channel_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/goware/channel"
)

func TestSlowProducer(t *testing.T) {
	testUnboundedBufferedChannel(t, 100*time.Millisecond, 0, 20)
}

func TestSlowConsumer(t *testing.T) {
	testUnboundedBufferedChannel(t, 0, 10*time.Millisecond, 100)
}

func TestClosed(t *testing.T) {
	ch := channel.NewUnboundedChan[int](10, 1000, channel.Options{Logger: slog.Default()})

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
	ch := channel.NewUnboundedChan[int](10, 20, channel.Options{Logger: slog.Default(), Label: "TestClosed"})

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
	ch := channel.NewUnboundedChan[string](5, 1000, channel.Options{Logger: slog.Default()})

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

	ch := channel.NewUnboundedChan[int](100, 100, channel.Options{Logger: slog.Default()})

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

func TestBlockedSend(t *testing.T) {
	ch := channel.NewUnboundedChan[int](2, 10, channel.Options{Logger: slog.Default()})

	ch.Send(1)
	ch.Send(2)
	ch.Send(3)
	ch.Close()
	ch.Flush()
	ch.Send(3)
}

func TestSendCloseRace(t *testing.T) {
	for i := 0; i < 2000; i++ {
		ch := channel.NewUnboundedChan[int](1000, 0)
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					ch.Send(j)
				}
			}()
		}
		ch.Close()
		wg.Wait()
		ch.Flush()
	}
}

func TestConcurrentClose(t *testing.T) {
	for i := 0; i < 2000; i++ {
		ch := channel.NewUnboundedChan[int](10, 0)
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch.Close()
			}()
		}
		wg.Wait()
		ch.Flush()
	}
}

func TestCloseDrainsQueuedMessages(t *testing.T) {
	const messages = 10
	ch := channel.NewUnboundedChan[int](100, 0)
	t.Cleanup(func() {
		done := make(chan struct{})
		go func() {
			ch.Close()
			ch.Flush()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("channel cleanup did not finish")
		}
	})

	closed := make(chan bool, 1)
	go func() {
		for i := 0; i < messages; i++ {
			ch.Send(i)
		}
		ch.Close()
		closed <- ch.Send(messages)
	}()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case accepted := <-closed:
		if accepted {
			t.Fatal("Send accepted a message after Close")
		}
	case <-timer.C:
		t.Fatal("Close or the subsequent Send blocked without a reader")
	}

	// Leave the closed queue without a reader before draining it. Closing must
	// preserve queued messages for a consumer that starts later.
	time.Sleep(20 * time.Millisecond)
	for want := 0; want < messages; want++ {
		select {
		case got, ok := <-ch.ReadChannel():
			if !ok || got != want {
				t.Fatalf("read (%d, %v), want (%d, true)", got, ok, want)
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for queued message %d", want)
		}
	}
	select {
	case got, ok := <-ch.ReadChannel():
		if ok {
			t.Fatalf("read extra message %d after draining the closed channel", got)
		}
	case <-timer.C:
		t.Fatal("read channel did not close after draining")
	}
}

type countingAlerter struct {
	mu     sync.Mutex
	alerts []string
}

func (a *countingAlerter) Alert(_ context.Context, format string, v ...interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.alerts = append(a.alerts, fmt.Sprintf(format, v...))
}

func (a *countingAlerter) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.alerts)
}

func TestBufferWarningBackoff(t *testing.T) {
	alerter := &countingAlerter{}
	ch := channel.NewUnboundedChan[int](2, 0, channel.Options{Alerter: alerter})
	defer func() {
		ch.Close()
		ch.Flush()
	}()

	// Nobody reads, so the queue grows to 100. Alerts fire as it passes 2,
	// then 6, 14, 30 and 62 messages, not on every message past 2.
	for i := 0; i < 100; i++ {
		ch.Send(i)
	}
	if got := alerter.count(); got != 5 {
		t.Fatalf("got %d alerts while the queue grew to 100, want 5", got)
	}

	// Draining the queue re-arms the warning. The fourth Send returns once the
	// third, which passes the limit again, has been queued.
	for i := 0; i < 100; i++ {
		<-ch.ReadChannel()
	}
	for i := 0; i < 4; i++ {
		ch.Send(i)
	}
	if got := alerter.count(); got != 6 {
		t.Fatalf("got %d alerts after the queue refilled, want 6", got)
	}
}

func TestCapacityDropAlert(t *testing.T) {
	alerter := &countingAlerter{}
	ch := channel.NewUnboundedChan[int](100, 5, channel.Options{Alerter: alerter})
	defer func() {
		ch.Close()
		ch.Flush()
	}()

	for i := 0; i < 10; i++ {
		ch.Send(i)
	}
	if got := alerter.count(); got != 1 {
		t.Fatalf("got %d alerts while dropping 5 messages, want 1", got)
	}
	for want := 5; want < 10; want++ {
		if msg := <-ch.ReadChannel(); msg != want {
			t.Fatalf("read %d, want %d: the oldest messages should be dropped", msg, want)
		}
	}
}
