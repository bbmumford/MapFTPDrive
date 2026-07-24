//go:build !windows

package credentials

import "errors"

type unsupportedStore struct{}

func newPlatformStore() Store { return unsupportedStore{} }

func (unsupportedStore) Put(string, string, string) error {
	return errors.New("Windows Credential Manager is unavailable")
}
func (unsupportedStore) Get(string) (string, string, error) {
	return "", "", errors.New("Windows Credential Manager is unavailable")
}
func (unsupportedStore) Delete(string) error {
	return errors.New("Windows Credential Manager is unavailable")
}
