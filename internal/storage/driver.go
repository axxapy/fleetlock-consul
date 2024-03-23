package storage

import (
	"context"
	"errors"
	"fmt"
)

type Driver interface {
	Lock(ctx context.Context, group, id string) error
	Unlock(ctx context.Context, group, id string) error
}

var (
	ErrLockNotFound  = errors.New("lock not found")
	ErrAlreadyLocked = errors.New("already locked")
)

const (
	keyTemplate = "com.coreos.zincati/%s"
)

func key(group string) string {
	return fmt.Sprintf(keyTemplate, group)
}
