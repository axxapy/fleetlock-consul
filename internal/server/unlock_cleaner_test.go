package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/axxapy/fleetlock-consul/internal/mocks"
	"go.uber.org/mock/gomock"
)

func TestUnlockCleaner_SuccessOnFirstAttempt(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	done := make(chan struct{})
	driver.EXPECT().Unlock(gomock.Any(), "default", "node-1").DoAndReturn(func(context.Context, string, string) error {
		close(done)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleaner := newUnlockCleaner(driver)
	go cleaner.Run(ctx)

	cleaner.Send("default", "node-1")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("unlock did not succeed within timeout")
	}
}

func TestUnlockCleaner_RetryUntilSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	done := make(chan struct{})
	gomock.InOrder(
		driver.EXPECT().Unlock(gomock.Any(), "default", "node-1").Return(fmt.Errorf("consul unavailable")),
		driver.EXPECT().Unlock(gomock.Any(), "default", "node-1").Return(fmt.Errorf("consul unavailable")),
		driver.EXPECT().Unlock(gomock.Any(), "default", "node-1").DoAndReturn(func(context.Context, string, string) error {
			close(done)
			return nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleaner := newUnlockCleaner(driver)
	go cleaner.Run(ctx)

	cleaner.Send("default", "node-1")

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("unlock did not succeed within timeout")
	}
}

func TestUnlockCleaner_MultipleRequests(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	count := 0
	done := make(chan struct{})
	driver.EXPECT().Unlock(gomock.Any(), "default", gomock.Any()).DoAndReturn(func(context.Context, string, string) error {
		count++
		if count == 2 {
			close(done)
		}
		return nil
	}).Times(2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleaner := newUnlockCleaner(driver)
	go cleaner.Run(ctx)

	cleaner.Send("default", "node-1")
	cleaner.Send("default", "node-2")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("unlocks did not complete within timeout")
	}
}

func TestUnlockCleaner_StopsOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	driver.EXPECT().Unlock(gomock.Any(), "default", "node-1").Return(fmt.Errorf("consul unavailable")).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())

	cleaner := newUnlockCleaner(driver)
	done := make(chan struct{})
	go func() {
		cleaner.Run(ctx)
		close(done)
	}()

	cleaner.Send("default", "node-1")
	time.Sleep(1 * time.Second)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleaner did not stop after context cancellation")
	}
}
