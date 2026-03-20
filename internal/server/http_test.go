package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestHandleFleetUnlock_ReturnsOKImmediately(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.HandleFleetUnlock(w, fleetlockRequest(`{"client_params":{"id":"node-1"}}`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
