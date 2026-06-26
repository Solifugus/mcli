package core

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringService is the service name under which mcli stores server secrets in
// the OS keyring (macOS Keychain, Windows Credential Manager, Linux Secret
// Service). Each server's secret is keyed by the server name.
const keyringService = "mcli"

// ErrPasswordRequired signals that a connection needs a password the core cannot
// obtain on its own — the source is "prompt", or it is "keyring" but no secret
// is stored / the keyring is unavailable (the documented headless fallback,
// design §7). A front-end catches this, prompts interactively, and retries via
// ConnectWithPassword.
var ErrPasswordRequired = errors.New("password required")

// keyringGet fetches a stored secret. A missing entry or an unavailable keyring
// (e.g. headless Linux with no Secret Service) both yield ok=false so the caller
// can fall back to prompting rather than failing.
func keyringGet(server string) (secret string, ok bool) {
	s, err := keyring.Get(keyringService, server)
	if err != nil {
		return "", false
	}
	return s, true
}

// SetServerPassword stores a server's secret in the OS keyring. It does not
// change the server's password_source; pair it with password_source "keyring".
func (c *Core) SetServerPassword(server, secret string) error {
	if _, ok := c.servers.Servers[server]; !ok {
		return fmt.Errorf("no server named %q", server)
	}
	if err := keyring.Set(keyringService, server, secret); err != nil {
		return fmt.Errorf("store secret in keyring: %w", err)
	}
	c.log("SERVER", "set-password", server)
	return nil
}

// DeleteServerPassword removes a server's secret from the keyring. A missing
// entry is not an error.
func (c *Core) DeleteServerPassword(server string) error {
	if err := keyring.Delete(keyringService, server); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete secret from keyring: %w", err)
	}
	c.log("SERVER", "clear-password", server)
	return nil
}
