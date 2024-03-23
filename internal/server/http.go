package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/axxapy/fleetlock-consul/internal"
	"github.com/axxapy/fleetlock-consul/internal/storage"
)

type (
	httpServer struct {
		defaultGroup  string
		storageDriver storage.Driver
		logger        *slog.Logger
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

func StartHttpServer(ctx context.Context, httpConfig internal.HttpServerConfig, defaultGroup string, storageDriver storage.Driver) error {
	server := &httpServer{
		defaultGroup:  defaultGroup,
		storageDriver: storageDriver,
		logger:        slog.Default().With("__source", "http_server"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pre-reboot", server.HandleFleetLock)
	mux.HandleFunc("/v1/steady-state", server.HandleFleetUnlock)

	errChan := make(chan error)

	for _, listen := range strings.Split(httpConfig.Listen, ",") {
		listen := strings.TrimSpace(listen)
		go func() {
			errChan <- (&http.Server{
				Addr:    listen,
				Handler: mux,
				BaseContext: func(listener net.Listener) context.Context {
					return ctx
				},
			}).ListenAndServe()
		}()
	}

	return <-errChan
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

	err := s.storageDriver.Lock(r.Context(), req.ClientParams.Group, req.ClientParams.Id)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(errorBody{
			Kind:  "failed_lock",
			Value: err.Error(),
		})

		s.logger.Error("failed to lock", "error", err)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *httpServer) HandleFleetUnlock(w http.ResponseWriter, r *http.Request) {
	req, ok := s.unmarshalRequest(w, r)
	if !ok {
		return
	}

	err := s.storageDriver.Unlock(r.Context(), req.ClientParams.Group, req.ClientParams.Id)
	if err != nil && !errors.Is(err, storage.ErrLockNotFound) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(errorBody{
			Kind:  "failed_unlock",
			Value: err.Error(),
		})

		s.logger.Error("failed to unlock", "error", err)

		return
	}

	w.WriteHeader(http.StatusOK)
}
