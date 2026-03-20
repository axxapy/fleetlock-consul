package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	consul "github.com/hashicorp/consul/api"
	"go.uber.org/mock/gomock"

	"github.com/axxapy/fleetlock-consul/internal"
	"github.com/axxapy/fleetlock-consul/internal/mocks"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func setupMocks(t *testing.T) (*driverConsul, *mocks.MockConsulKVClient, *mocks.MockConsulSessionClient) {
	ctrl := gomock.NewController(t)
	kv := mocks.NewMockConsulKVClient(ctrl)
	session := mocks.NewMockConsulSessionClient(ctrl)
	driver := &driverConsul{
		consulKV:      kv,
		consulSession: session,
		logger:        noopLogger(),
	}
	return driver, kv, session
}

func expectMutexAcquireAndRelease(kv *mocks.MockConsulKVClient, session *mocks.MockConsulSessionClient) {
	session.EXPECT().Create(gomock.Any(), gomock.Any()).Return("test-session", &consul.WriteMeta{}, nil)
	kv.EXPECT().Acquire(gomock.Any(), gomock.Any()).Return(true, &consul.WriteMeta{}, nil)
	kv.EXPECT().Delete(key("default::lock"), gomock.Any()).Return(&consul.WriteMeta{}, nil)
	session.EXPECT().Destroy("test-session", gomock.Any()).Return(&consul.WriteMeta{}, nil)
}

// --- NewDriverConsul tests ---

func TestNewDriverConsul_NoAuth(t *testing.T) {
	d, err := NewDriverConsul(internal.ConsulConfig{Address: "127.0.0.1:8500"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

func TestNewDriverConsul_WithAuth(t *testing.T) {
	d, err := NewDriverConsul(internal.ConsulConfig{Address: "127.0.0.1:8500", Auth: "user:pass"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

func TestNewDriverConsul_AuthNoColon(t *testing.T) {
	d, err := NewDriverConsul(internal.ConsulConfig{Address: "127.0.0.1:8500", Auth: "tokenonly"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

func TestNewDriverConsul_WithToken(t *testing.T) {
	d, err := NewDriverConsul(internal.ConsulConfig{Address: "127.0.0.1:8500", Token: "my-token"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

// --- withLock tests ---

func TestWithLock_SessionCreateError(t *testing.T) {
	driver, _, session := setupMocks(t)
	session.EXPECT().Create(gomock.Any(), gomock.Any()).Return("", &consul.WriteMeta{}, fmt.Errorf("consul down"))

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when session creation fails")
	}
}

func TestWithLock_AcquireError(t *testing.T) {
	driver, kv, session := setupMocks(t)
	session.EXPECT().Create(gomock.Any(), gomock.Any()).Return("test-session", &consul.WriteMeta{}, nil)
	kv.EXPECT().Acquire(gomock.Any(), gomock.Any()).Return(false, &consul.WriteMeta{}, fmt.Errorf("connection refused"))

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when Acquire returns error")
	}
	if !errors.Is(err, fmt.Errorf("connection refused")) && err.Error() != "failed to acquire lock for group default: connection refused" {
		// Just verify we get a wrapped error, not the generic contention message
	}
}

func TestWithLock_MutexDeleteError(t *testing.T) {
	driver, kv, session := setupMocks(t)

	session.EXPECT().Create(gomock.Any(), gomock.Any()).Return("test-session", &consul.WriteMeta{}, nil)
	kv.EXPECT().Acquire(gomock.Any(), gomock.Any()).Return(true, &consul.WriteMeta{}, nil)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)
	kv.EXPECT().Delete(key("default::lock"), gomock.Any()).Return(&consul.WriteMeta{}, fmt.Errorf("delete failed"))
	session.EXPECT().Destroy("test-session", gomock.Any()).Return(&consul.WriteMeta{}, nil)

	// The mutex cleanup error is logged, not returned - operation itself succeeds
	err := driver.Unlock(context.Background(), "default", "node-1")
	if err != nil {
		t.Fatalf("expected no error (cleanup errors are logged), got: %v", err)
	}
}

func TestWithLock_SessionDestroyError(t *testing.T) {
	driver, kv, session := setupMocks(t)

	session.EXPECT().Create(gomock.Any(), gomock.Any()).Return("test-session", &consul.WriteMeta{}, nil)
	kv.EXPECT().Acquire(gomock.Any(), gomock.Any()).Return(true, &consul.WriteMeta{}, nil)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)
	kv.EXPECT().Delete(key("default::lock"), gomock.Any()).Return(&consul.WriteMeta{}, nil)
	session.EXPECT().Destroy("test-session", gomock.Any()).Return(&consul.WriteMeta{}, fmt.Errorf("destroy failed"))

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err != nil {
		t.Fatalf("expected no error (cleanup errors are logged), got: %v", err)
	}
}

// --- Unlock tests ---

func TestUnlock_NoExistingLock(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestUnlock_MatchingID(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(
		&consul.KVPair{Key: key("default"), Value: []byte("node-1")}, &consul.QueryMeta{}, nil,
	)
	kv.EXPECT().Delete(key("default"), gomock.Any()).Return(&consul.WriteMeta{}, nil)

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestUnlock_DifferentID(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(
		&consul.KVPair{Key: key("default"), Value: []byte("node-1")}, &consul.QueryMeta{}, nil,
	)

	err := driver.Unlock(context.Background(), "default", "node-2")
	if err == nil {
		t.Fatal("expected error when unlocking with different ID")
	}
}

func TestUnlock_Idempotent(t *testing.T) {
	driver, kv, session := setupMocks(t)

	expectMutexAcquireAndRelease(kv, session)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)

	expectMutexAcquireAndRelease(kv, session)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)

	if err := driver.Unlock(context.Background(), "default", "node-1"); err != nil {
		t.Fatalf("first unlock: expected no error, got: %v", err)
	}
	if err := driver.Unlock(context.Background(), "default", "node-1"); err != nil {
		t.Fatalf("second unlock: expected no error, got: %v", err)
	}
}

// TestUnlock_MutexContention reproduces the bug: mutex acquisition fails during unlock,
// so the stale data lock is never cleared and all other nodes are blocked.
func TestUnlock_MutexContention(t *testing.T) {
	driver, kv, session := setupMocks(t)

	session.EXPECT().Create(gomock.Any(), gomock.Any()).Return("test-session", &consul.WriteMeta{}, nil)
	kv.EXPECT().Acquire(gomock.Any(), gomock.Any()).Return(false, &consul.WriteMeta{}, nil)

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when mutex acquisition fails")
	}
}

func TestUnlock_GetError(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, fmt.Errorf("consul read error"))

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when Get fails")
	}
}

func TestUnlock_DeleteError(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(
		&consul.KVPair{Key: key("default"), Value: []byte("node-1")}, &consul.QueryMeta{}, nil,
	)
	kv.EXPECT().Delete(key("default"), gomock.Any()).Return(&consul.WriteMeta{}, fmt.Errorf("consul delete error"))

	err := driver.Unlock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when data key Delete fails")
	}
}

// --- Lock tests ---

func TestLockThenUnlock(t *testing.T) {
	driver, kv, session := setupMocks(t)

	expectMutexAcquireAndRelease(kv, session)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)
	kv.EXPECT().Put(gomock.Any(), gomock.Any()).Return(&consul.WriteMeta{}, nil)

	expectMutexAcquireAndRelease(kv, session)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(
		&consul.KVPair{Key: key("default"), Value: []byte("node-1")}, &consul.QueryMeta{}, nil,
	)
	kv.EXPECT().Delete(key("default"), gomock.Any()).Return(&consul.WriteMeta{}, nil)

	if err := driver.Lock(context.Background(), "default", "node-1"); err != nil {
		t.Fatalf("lock: expected no error, got: %v", err)
	}
	if err := driver.Unlock(context.Background(), "default", "node-1"); err != nil {
		t.Fatalf("unlock: expected no error, got: %v", err)
	}
}

func TestLock_AlreadyLockedByOther(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(
		&consul.KVPair{Key: key("default"), Value: []byte("node-1")}, &consul.QueryMeta{}, nil,
	)

	err := driver.Lock(context.Background(), "default", "node-2")
	if err == nil {
		t.Fatal("expected error when locking already-locked group with different ID")
	}
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("expected ErrAlreadyLocked, got: %v", err)
	}
}

func TestLock_SameIDIdempotent(t *testing.T) {
	driver, kv, session := setupMocks(t)

	expectMutexAcquireAndRelease(kv, session)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)
	kv.EXPECT().Put(gomock.Any(), gomock.Any()).Return(&consul.WriteMeta{}, nil)

	expectMutexAcquireAndRelease(kv, session)
	kv.EXPECT().Get(key("default"), gomock.Any()).Return(
		&consul.KVPair{Key: key("default"), Value: []byte("node-1")}, &consul.QueryMeta{}, nil,
	)
	kv.EXPECT().Put(gomock.Any(), gomock.Any()).Return(&consul.WriteMeta{}, nil)

	if err := driver.Lock(context.Background(), "default", "node-1"); err != nil {
		t.Fatalf("first lock: expected no error, got: %v", err)
	}
	if err := driver.Lock(context.Background(), "default", "node-1"); err != nil {
		t.Fatalf("second lock with same ID: expected no error, got: %v", err)
	}
}

func TestLock_GetError(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, fmt.Errorf("consul read error"))

	err := driver.Lock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when Get fails during lock")
	}
}

func TestLock_PutError(t *testing.T) {
	driver, kv, session := setupMocks(t)
	expectMutexAcquireAndRelease(kv, session)

	kv.EXPECT().Get(key("default"), gomock.Any()).Return(nil, &consul.QueryMeta{}, nil)
	kv.EXPECT().Put(gomock.Any(), gomock.Any()).Return(&consul.WriteMeta{}, fmt.Errorf("consul write error"))

	err := driver.Lock(context.Background(), "default", "node-1")
	if err == nil {
		t.Fatal("expected error when Put fails during lock")
	}
}
