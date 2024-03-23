package storage

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/axxapy/fleetlock-consul/internal"

	consul "github.com/hashicorp/consul/api"
)

type driverConsul struct {
	logger *slog.Logger

	consulKV      *consul.KV
	consulSession *consul.Session
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
		consulKV:      client.KV(),
		consulSession: client.Session(),
		logger:        slog.Default().With("__source", "driver_consul"),
	}, nil
}

func (d *driverConsul) withLock(ctx context.Context, group, id string, f func() error) error {
	keyName := key(group + "::lock")
	consulArgs := new(consul.WriteOptions).WithContext(ctx)

	sessionID, _, err := d.consulSession.Create(&consul.SessionEntry{
		Name:     keyName,
		Behavior: "delete",
		TTL:      "15s",
	}, consulArgs)
	if err != nil {
		d.logger.Error("failed to create session", "error", err)
		return err
	}

	kv := &consul.KVPair{Key: keyName, Value: []byte(id), Session: sessionID}
	var repeatCounter int
again:
	ok, _, err := d.consulKV.Acquire(kv, consulArgs)
	if err != nil {
		d.logger.Error("failed to acquire lock", "group", group, "id", id, "error", err)
	}
	if !ok {
		/* https://developer.hashicorp.com/consul/docs/dynamic-app-config/sessions
		 * The final nuance is that sessions may provide a lock-delay . This is a time duration, between 0 and 60 seconds.
		 * When a session invalidation takes place, Consul prevents any of the previously held locks from being re-acquired
		 *   for the lock-delay interval; this is a safeguard inspired by Google's Chubby. */
		repeatCounter++
		if repeatCounter > 60 {
			return fmt.Errorf("failed to acquire lock for group %s with id %s", group, id)
		}
		time.Sleep(time.Second)
		goto again
	}

	defer func() {
		_, err := d.consulSession.Destroy(sessionID, new(consul.WriteOptions).WithContext(ctx))
		//ok, _, err := d.consulKV.Release(kv, new(consul.WriteOptions).WithContext(ctx))
		if err != nil {
			d.logger.Error("failed to release lock", "group", group, "session_id", sessionID, "error", err)
		}
		if !ok {
			d.logger.Error("failed to unlock group", "group", group, "session_id", sessionID, "error", err)
		}
	}()

	return f()
}

func (d *driverConsul) Lock(ctx context.Context, group, id string) error {
	return d.withLock(ctx, group, id, func() error {
		keyName := key(group)

		pair, _, err := d.consulKV.Get(keyName, new(consul.QueryOptions).WithContext(ctx))
		if err != nil {
			d.logger.Error("failed to get group record", "group", group, "id", id, "error", err)
			return err
		}

		if pair != nil && string(pair.Value) != id {
			return fmt.Errorf("%w: group %s already locked by %s", ErrAlreadyLocked, group, string(pair.Value))
		}

		kv := &consul.KVPair{Key: keyName, Value: []byte(id)}
		if _, err := d.consulKV.Put(kv, new(consul.WriteOptions).WithContext(ctx)); err != nil {
			d.logger.Error("failed to put key", "key", keyName, "error", err)
			return err
		}

		return nil
	})
}

func (d *driverConsul) Unlock(ctx context.Context, group, id string) error {
	return d.withLock(ctx, group, id, func() error {
		keyName := key(group)

		pair, _, err := d.consulKV.Get(keyName, new(consul.QueryOptions).WithContext(ctx))
		if err != nil {
			d.logger.Error("failed to get group record", "group", group, "id", id, "error", err)
			return err
		}

		if pair == nil {
			return nil
		}

		if string(pair.Value) != id {
			return fmt.Errorf("client with %s can not unlock group %s that was locked by %s", id, group, string(pair.Value))
		}

		if _, err := d.consulKV.Delete(keyName, new(consul.WriteOptions).WithContext(ctx)); err != nil {
			d.logger.Error("failed to delete key", "key", keyName, "error", err)
			return err
		}

		return nil
	})
}
