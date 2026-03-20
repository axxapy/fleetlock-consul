package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axxapy/fleetlock-consul/internal"
	"github.com/axxapy/fleetlock-consul/internal/server"
	"github.com/axxapy/fleetlock-consul/internal/storage"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("fleetlock-consul " + version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	}))
	slog.SetDefault(logger)

	check := func(desc string, err error) {
		if err != nil {
			logger.Error(desc, "error", err)
			os.Exit(1)
		}
	}

	c, err := internal.ParseConfig()
	check("failed to parse config", err)

	storageDriver, err := storage.NewDriverConsul(c.Consul)
	check("failed to create storage driver", err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	servers, err := server.StartHttpServer(ctx, c.Http, c.DefaultGroup, storageDriver)
	check("failed to start http server", err)

	logger.Info("server started", "listen", c.Http.Listen)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig

	logger.Info("got signal, shutting down", "signal", s)

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	for _, hs := range servers {
		shutdown(hs, shutdownCtx)
	}

	logger.Info("shutdown complete")
}

func shutdown(hs *http.Server, ctx context.Context) {
	if err := hs.Shutdown(ctx); err != nil {
		slog.Error("http server shutdown error", "addr", hs.Addr, "error", err)
	}
}
