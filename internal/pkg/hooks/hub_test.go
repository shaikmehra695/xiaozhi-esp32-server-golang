package hooks

import (
	"context"
	"testing"
	"time"
)

func TestAsyncHandlersPreserveEmitOrder(t *testing.T) {
	hub := NewHub(context.Background())
	defer hub.Close()

	got := make(chan int, 3)
	hub.RegisterAsync("metric", "collector", 100, func(ctx Context, payload any) {
		got <- payload.(int)
	})

	for i := 0; i < 3; i++ {
		if _, _, err := hub.Emit("metric", Context{}, i); err != nil {
			t.Fatalf("Emit() error = %v", err)
		}
	}

	want := []int{0, 1, 2}
	for i, expected := range want {
		select {
		case actual := <-got:
			if actual != expected {
				t.Fatalf("result[%d] = %d, want %d", i, actual, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for async handler result %d", i)
		}
	}
}

func TestAsyncEmitDoesNotBlockWhenHandlersAreSlow(t *testing.T) {
	hub := NewHub(context.Background())
	defer hub.Close()

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	hub.RegisterAsync("metric", "slow", 100, func(ctx Context, payload any) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 600; i++ {
			if _, _, err := hub.Emit("metric", Context{}, i); err != nil {
				t.Errorf("Emit() error = %v", err)
				return
			}
		}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for async handler to start")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit() blocked while async handler was slow")
	}

	close(release)
}
