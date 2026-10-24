package keystore

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	service = "whisker"
	user    = "store-key"
)

// SecretService is the Backend on the freedesktop Secret Service.
type SecretService struct{}

func (SecretService) Get() (string, error) { return mapErr(keyring.Get(service, user)) }

func (SecretService) Set(value string) error {
	_, err := mapErr("", keyring.Set(service, user, value))
	return err
}

func (SecretService) Delete() error {
	_, err := mapErr("", keyring.Delete(service, user))
	return err
}

func mapErr(value string, err error) (string, error) {
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return value, err
}
