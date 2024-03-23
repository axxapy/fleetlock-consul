package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axxapy/fleetlock-consul/internal"
	"github.com/axxapy/fleetlock-consul/internal/server"
	"github.com/axxapy/fleetlock-consul/internal/storage"
)

func main() {
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		err := server.StartHttpServer(ctx, c.Http, c.DefaultGroup, storageDriver)
		check("failed to start http server", err)

		logger.Info("http server started", "listen", c.Http.Listen)

		<-ctx.Done()
	}()

	logger.Info("server started")

	s := <-sig

	logger.Info("got signal, shutting down (5 seconds)", "signal", s)

	cancel()
	<-time.After(5 * time.Second)

	logger.Info("shutdown complete")
}
