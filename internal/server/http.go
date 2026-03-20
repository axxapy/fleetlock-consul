package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/axxapy/fleetlock-consul/internal"
	"github.com/axxapy/fleetlock-consul/internal/storage"
)

type (
	httpServer struct {
		defaultGroup   string
		storageDriver  storage.Driver
		unlockCleaner  *unlockCleaner
		logger         *slog.Logger
	}

	requestBody struct {
		ClientParams struct {
			Group string `json:"group"`
			Id    string `json:"id"`
		} `json:"client_params"`
	}

	errorBody struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
)

func StartHttpServer(ctx context.Context, httpConfig internal.HttpServerConfig, defaultGroup string, storageDriver storage.Driver) ([]*http.Server, error) {
	cleaner := newUnlockCleaner(storageDriver)
	go cleaner.Run(ctx)

	srv := &httpServer{
		defaultGroup:  defaultGroup,
		storageDriver: storageDriver,
		unlockCleaner: cleaner,
		logger:        slog.Default().With("__source", "http_server"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pre-reboot", srv.HandleFleetLock)
	mux.HandleFunc("/v1/steady-state", srv.HandleFleetUnlock)

	var servers []*http.Server
	errChan := make(chan error, 1)

	for _, listen := range strings.Split(httpConfig.Listen, ",") {
		listen = strings.TrimSpace(listen)
		hs := &http.Server{
			Addr:    listen,
			Handler: mux,
			BaseContext: func(listener net.Listener) context.Context {
				return ctx
			},
		}
		servers = append(servers, hs)
		go func() {
			if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				select {
				case errChan <- err:
				default:
				}
			}
		}()
	}

	// Give listeners a moment to fail (e.g. port already in use)
	select {
	case err := <-errChan:
		return nil, err
	case <-time.After(50 * time.Millisecond):
	}

	return servers, nil
}

func (s *httpServer) unmarshalRequest(w http.ResponseWriter, r *http.Request) (*requestBody, bool) {
	if r.Header.Get("fleet-lock-protocol") != "true" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorBody{
			Kind:  "missing_header",
			Value: "'fleet-lock-protocol' header is missing or invalid",
		})

		s.logger.Error("missing or invalid 'fleet-lock-protocol' header")

		return nil, false
	}

	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorBody{
			Kind:  "invalid_body",
			Value: err.Error(),
		})

		s.logger.Error("failed to decode request body", "error", err)

		return nil, false
	}

	if body.ClientParams.Group == "" {
		body.ClientParams.Group = s.defaultGroup
	}

	if body.ClientParams.Id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorBody{
			Kind:  "missing_id",
			Value: "client ID is empty",
		})

		s.logger.Error("client ID is empty")

		return nil, false
	}

	return &body, true
}

func (s *httpServer) HandleFleetLock(w http.ResponseWriter, r *http.Request) {
	req, ok := s.unmarshalRequest(w, r)
	if !ok {
		return
	}

	const (
		lockTimeout    = 30 * time.Second
		initialBackoff = 200 * time.Millisecond
		maxBackoff     = 5 * time.Second
	)

	ctx, cancel := context.WithTimeout(r.Context(), lockTimeout)
	defer cancel()

	backoff := initialBackoff
	for {
		err := s.storageDriver.Lock(ctx, req.ClientParams.Group, req.ClientParams.Id)
		if err == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Data lock held by another node - legitimate rejection, don't retry
		if errors.Is(err, storage.ErrAlreadyLocked) {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(errorBody{
				Kind:  "failed_lock",
				Value: err.Error(),
			})
			s.logger.Error("failed to lock", "error", err)
			return
		}

		// Transient error (mutex contention, consul unavailable) - retry
		s.logger.Warn("lock failed, retrying", "group", req.ClientParams.Group, "id", req.ClientParams.Id, "error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(errorBody{
				Kind:  "failed_lock",
				Value: err.Error(),
			})
			s.logger.Error("failed to lock after retries", "error", err)
			return
		case <-time.After(backoff):
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

func (s *httpServer) HandleFleetUnlock(w http.ResponseWriter, r *http.Request) {
	req, ok := s.unmarshalRequest(w, r)
	if !ok {
		return
	}

	s.unlockCleaner.Send(req.ClientParams.Group, req.ClientParams.Id)
	w.WriteHeader(http.StatusOK)
}
