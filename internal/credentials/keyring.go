package credentials

import (
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const keyringService = "termrouter"

// rest is "service/user" or just "name" for keyring://termrouter/name
func resolveKeyring(rest string) (string, error) {
	service, user := keyringService, rest
	if strings.Contains(rest, "/") {
		parts := strings.SplitN(rest, "/", 2)
		service, user = parts[0], parts[1]
	}
	secret, err := keyring.Get(service, user)
	if err != nil {
		return "", fmt.Errorf("keyring get %s/%s: %w", service, user, err)
	}
	return secret, nil
}

func storeKeyring(name, secret string) error {
	return keyring.Set(keyringService, name, secret)
}

func removeKeyring(rest string) error {
	service, user := keyringService, rest
	if strings.Contains(rest, "/") {
		parts := strings.SplitN(rest, "/", 2)
		service, user = parts[0], parts[1]
	}
	return keyring.Delete(service, user)
}
