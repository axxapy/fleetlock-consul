package storage

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/axxapy/fleetlock-consul/internal"

	consul "github.com/hashicorp/consul/api"
)

type driverConsul struct {
	client *consul.Client
	logger *slog.Logger
}

func NewDriverConsul(config internal.ConsulConfig) (Driver, error) {
	clientConfig := consul.DefaultConfig()
	clientConfig.Address = config.Address
	clientConfig.Token = config.Token
	if config.Auth != "" {
		tmp := strings.SplitN(config.Auth, ":", 2)
		if len(tmp) == 2 {
			clientConfig.HttpAuth = &consul.HttpBasicAuth{
				Username: tmp[0],
				Password: tmp[1],
			}
		}
	}

	client, err := consul.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %w", err)
	}

	return &driverConsul{
		client: client,
		logger: slog.Default().With("go_file", func() string {
			_, file, _, _ := runtime.Caller(1)
			return filepath.Base(file)
		}()),
	}, nil
}

func (d *driverConsul) Lock(ctx context.Context, group, id string) error {
	consulArgs := new(consul.WriteOptions).WithContext(ctx)
	keyName := key(group)

	sessionID, _, err := d.client.Session().Create(&consul.SessionEntry{
		Name:     keyName,
		Behavior: "delete",
	}, consulArgs)
	if err != nil {
		d.logger.Error("failed to create session", "error", err)
		return err
	}

	kv := &consul.KVPair{Key: keyName, Value: []byte(id), Session: sessionID}
	ok, _, err := d.client.KV().Acquire(kv, consulArgs)
	if err != nil {
		d.logger.Error("failed to acquire lock", "group", group, "id", id, "error", err)
	}
	if !ok {
		return fmt.Errorf("failed to acquire lock for group %s with id %s", group, id)
	}
	return err
}

func (d *driverConsul) Unlock(ctx context.Context, group, id string) error {
	keyName := key(group)

	pair, _, err := d.client.KV().Get(keyName, new(consul.QueryOptions).WithContext(ctx))
	if err != nil {
		d.logger.Error("failed to find session that locked", "group", group, "id", id, "error", err)
		return err
	}

	if pair == nil || pair.Session == "" {
		return fmt.Errorf("%w: failed to find session that locked for group %s with id %s", ErrLockNotFound, group, id)
	}

	sessionID := pair.Session

	kv := &consul.KVPair{Key: keyName, Value: []byte(id), Session: sessionID}
	ok, _, err := d.client.KV().Release(kv, new(consul.WriteOptions).WithContext(ctx))
	if err != nil {
		d.logger.Error("failed to release lock", "group", group, "id", id, "error", err)
	}
	if !ok {
		return fmt.Errorf("failed to release lock for group %s with id %s", group, id)
	}

	if _, err := d.client.KV().Delete(keyName, new(consul.WriteOptions).WithContext(ctx)); err != nil {
		d.logger.Error("failed to delete key", "key", keyName, "error", err)
	}

	return err
}
