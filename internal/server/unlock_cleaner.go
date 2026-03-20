package server

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/axxapy/fleetlock-consul/internal/storage"
)

type unlockRequest struct {
	Group string
	ID    string
}

type unlockCleaner struct {
	driver  storage.Driver
	logger  *slog.Logger
	reqChan chan unlockRequest

	pending      []unlockRequest
	pendingCount atomic.Uint32
	mu           sync.Mutex
}

func newUnlockCleaner(driver storage.Driver) *unlockCleaner {
	return &unlockCleaner{
		driver:  driver,
		logger:  slog.Default().With("__source", "unlock_cleaner"),
		reqChan: make(chan unlockRequest, 64),
	}
}

func (c *unlockCleaner) Send(group, id string) {
	c.reqChan <- unlockRequest{Group: group, ID: id}
}

func (c *unlockCleaner) Run(ctx context.Context) {
	go func() {
		for {
			select {
			case req := <-c.reqChan:
				c.mu.Lock()
				c.pending = append(c.pending, req)
				c.pendingCount.Store(uint32(len(c.pending)))
				c.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	const (
		initialBackoff = 500 * time.Millisecond
		maxBackoff     = 30 * time.Second
		tickInterval   = 500 * time.Millisecond
	)

	backoff := initialBackoff
	nextTry := time.Now()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			if now.Before(nextTry) {
				continue
			}

			for c.pendingCount.Load() > 0 {
				c.mu.Lock()
				req := c.pending[0]
				c.mu.Unlock()

				err := c.driver.Unlock(ctx, req.Group, req.ID)
				if err != nil {
					backoff = min(backoff*2, maxBackoff)
					nextTry = now.Add(backoff)
					break
				}

				nextTry = now
				backoff = initialBackoff

				c.mu.Lock()
				c.pending = c.pending[1:]
				c.pendingCount.Store(uint32(len(c.pending)))
				if len(c.pending) < 1 {
					c.pending = nil
				}
				c.mu.Unlock()
			}
		}
	}
}
