package storage

import (
	"context"
	"errors"
	"fmt"
)

//go:generate mockgen -destination=../mocks/mock_driver.go -package=mocks github.com/axxapy/fleetlock-consul/internal/storage Driver

type Driver interface {
	Lock(ctx context.Context, group, id string) error
	Unlock(ctx context.Context, group, id string) error
}

var ErrAlreadyLocked = errors.New("already locked")

const (
	keyTemplate = "com.coreos.fleetlock/%s"
)

func key(group string) string {
	return fmt.Sprintf(keyTemplate, group)
}
