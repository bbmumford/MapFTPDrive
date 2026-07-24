package credentials

import "errors"

var ErrNotFound = errors.New("credential not found")

type Store interface {
	Put(key, username, password string) error
	Get(key string) (username, password string, err error)
	Delete(key string) error
}

func NewStore() Store {
	return newPlatformStore()
}
