package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/config"
)

// Server returns a configured server profile by name.
func (c *Core) Server(name string) (config.Server, bool) {
	s, ok := c.servers.Servers[name]
	return s, ok
}

// ServerNames returns the configured server names, sorted.
func (c *Core) ServerNames() []string {
	names := make([]string, 0, len(c.servers.Servers))
	for n := range c.servers.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// AddServer registers a new server profile and persists servers.json. It errors
// if the name is taken or the profile is invalid.
func (c *Core) AddServer(name string, s config.Server) error {
	name = strings.TrimSpace(name)
	if err := validateServer(name, s); err != nil {
		return err
	}
	if _, exists := c.servers.Servers[name]; exists {
		return fmt.Errorf("server %q already exists (use .server edit)", name)
	}
	c.putServer(name, s)
	if err := c.cfg.SaveServers(c.servers); err != nil {
		return err
	}
	c.log("SERVER", "add", name)
	return nil
}

// EditServer replaces an existing server profile and persists servers.json.
func (c *Core) EditServer(name string, s config.Server) error {
	name = strings.TrimSpace(name)
	if _, exists := c.servers.Servers[name]; !exists {
		return fmt.Errorf("no server named %q", name)
	}
	if err := validateServer(name, s); err != nil {
		return err
	}
	c.putServer(name, s)
	if err := c.cfg.SaveServers(c.servers); err != nil {
		return err
	}
	c.log("SERVER", "edit", name)
	return nil
}

// RemoveServer deletes a server profile and persists servers.json. The currently
// connected server cannot be removed; disconnect first.
func (c *Core) RemoveServer(name string) error {
	if _, exists := c.servers.Servers[name]; !exists {
		return fmt.Errorf("no server named %q", name)
	}
	if name == c.connServer {
		return fmt.Errorf("server %q is connected; .disconnect first", name)
	}
	delete(c.servers.Servers, name)
	if err := c.cfg.SaveServers(c.servers); err != nil {
		return err
	}
	c.log("SERVER", "remove", name)
	return nil
}

// TestServer opens a throwaway connection to verify a server is reachable and the
// credentials resolve, then closes it. It never disturbs the live connection. If
// the password source needs interactive entry it returns ErrPasswordRequired, and
// the caller should retry via TestServerWith.
func (c *Core) TestServer(ctx context.Context, name string) error {
	srv, ok := c.servers.Servers[name]
	if !ok {
		return fmt.Errorf("no server named %q", name)
	}
	password, err := resolvePassword(name, srv)
	if err != nil {
		return err
	}
	return c.testWith(ctx, srv, password)
}

// TestServerWith verifies reachability using an explicitly supplied password.
func (c *Core) TestServerWith(ctx context.Context, name, password string) error {
	srv, ok := c.servers.Servers[name]
	if !ok {
		return fmt.Errorf("no server named %q", name)
	}
	return c.testWith(ctx, srv, password)
}

func (c *Core) testWith(ctx context.Context, srv config.Server, password string) error {
	ad, err := c.dialAdapter(ctx, srv, password)
	if err != nil {
		return err
	}
	return ad.Disconnect()
}

// putServer stores a profile, allocating the map on first use.
func (c *Core) putServer(name string, s config.Server) {
	if c.servers.Servers == nil {
		c.servers.Servers = map[string]config.Server{}
	}
	c.servers.Servers[name] = s
}

// validateServer enforces the minimum a profile needs to be usable: a name and a
// known (registered) database type. Per-driver connection details are validated
// at connect time by the adapter, which owns those rules.
func validateServer(name string, s config.Server) error {
	if name == "" {
		return fmt.Errorf("server name is required")
	}
	if s.Type == "" {
		return fmt.Errorf("server type is required (one of: %s)", strings.Join(adapter.Types(), ", "))
	}
	if !adapter.Registered(s.Type) {
		return fmt.Errorf("unknown database type %q (one of: %s)", s.Type, strings.Join(adapter.Types(), ", "))
	}
	return nil
}
