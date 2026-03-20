package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axxapy/fleetlock-consul/internal"
	"github.com/axxapy/fleetlock-consul/internal/mocks"
	"github.com/axxapy/fleetlock-consul/internal/storage"
	"go.uber.org/mock/gomock"
)

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func newTestServer(t *testing.T) (*httpServer, *mocks.MockDriver) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	cleaner := newUnlockCleaner(driver)
	return &httpServer{
		defaultGroup:  "default",
		storageDriver: driver,
		unlockCleaner: cleaner,
		logger:        slog.Default(),
	}, driver
}

func fleetlockRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.Header.Set("fleet-lock-protocol", "true")
	return req
}

// --- StartHttpServer tests ---

func TestStartHttpServer_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	servers, err := StartHttpServer(ctx, internal.HttpServerConfig{Listen: "127.0.0.1:0"}, "default", driver)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}

	cancel()
	for _, s := range servers {
		s.Close()
	}
}

func TestStartHttpServer_InvalidAddr(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := StartHttpServer(ctx, internal.HttpServerConfig{Listen: "127.0.0.1:-1"}, "default", driver)
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestStartHttpServer_MultipleListeners(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	servers, err := StartHttpServer(ctx, internal.HttpServerConfig{Listen: "127.0.0.1:0, 127.0.0.1:0"}, "default", driver)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}

	cancel()
	for _, s := range servers {
		s.Close()
	}
}

// --- HandleFleetLock tests ---

func TestHandleFleetLock_Success(t *testing.T) {
	srv, driver := newTestServer(t)
	driver.EXPECT().Lock(gomock.Any(), "default", "node-1").Return(nil)

	w := httptest.NewRecorder()
	srv.HandleFleetLock(w, fleetlockRequest(`{"client_params":{"id":"node-1"}}`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleFleetLock_AlreadyLocked(t *testing.T) {
	srv, driver := newTestServer(t)
	driver.EXPECT().Lock(gomock.Any(), "default", "node-2").Return(
		fmt.Errorf("%w: group default already locked by node-1", storage.ErrAlreadyLocked),
	)

	w := httptest.NewRecorder()
	srv.HandleFleetLock(w, fleetlockRequest(`{"client_params":{"id":"node-2"}}`))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}

	var body errorBody
	json.NewDecoder(w.Body).Decode(&body)
	if body.Kind != "failed_lock" {
		t.Fatalf("expected kind 'failed_lock', got %q", body.Kind)
	}
}

func TestHandleFleetLock_TransientErrorThenSuccess(t *testing.T) {
	srv, driver := newTestServer(t)

	gomock.InOrder(
		driver.EXPECT().Lock(gomock.Any(), "default", "node-1").Return(fmt.Errorf("mutex contention")),
		driver.EXPECT().Lock(gomock.Any(), "default", "node-1").Return(nil),
	)

	w := httptest.NewRecorder()
	srv.HandleFleetLock(w, fleetlockRequest(`{"client_params":{"id":"node-1"}}`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after retry, got %d", w.Code)
	}
}

func TestHandleFleetLock_TimeoutAfterRetries(t *testing.T) {
	srv, driver := newTestServer(t)

	// Always return transient error so retries exhaust the timeout
	driver.EXPECT().Lock(gomock.Any(), "default", "node-1").Return(fmt.Errorf("consul unavailable")).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled - ctx.Done() fires immediately on first retry

	w := httptest.NewRecorder()
	req := fleetlockRequest(`{"client_params":{"id":"node-1"}}`)
	req = req.WithContext(ctx)
	srv.HandleFleetLock(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 after timeout, got %d", w.Code)
	}

	var body errorBody
	json.NewDecoder(w.Body).Decode(&body)
	if body.Kind != "failed_lock" {
		t.Fatalf("expected kind 'failed_lock', got %q", body.Kind)
	}
}

func TestHandleFleetLock_MissingHeader(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"client_params":{"id":"node-1"}}`))
	srv.HandleFleetLock(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleFleetLock_MissingID(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.HandleFleetLock(w, fleetlockRequest(`{"client_params":{"group":"default"}}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleFleetLock_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.HandleFleetLock(w, fleetlockRequest(`not json`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var body errorBody
	json.NewDecoder(w.Body).Decode(&body)
	if body.Kind != "invalid_body" {
		t.Fatalf("expected kind 'invalid_body', got %q", body.Kind)
	}
}

// --- HandleFleetUnlock tests ---

func TestHandleFleetUnlock_ReturnsOKImmediately(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.HandleFleetUnlock(w, fleetlockRequest(`{"client_params":{"id":"node-1"}}`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleFleetUnlock_MissingHeader(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"client_params":{"id":"node-1"}}`))
	srv.HandleFleetUnlock(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
