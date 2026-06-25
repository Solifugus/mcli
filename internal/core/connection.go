package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/config"
)

// ErrNotConnected is returned by database operations when no server is connected.
var ErrNotConnected = errors.New("not connected to a server (use \\connect)")

// Servers returns the configured server registry.
func (c *Core) Servers() map[string]config.Server { return c.servers.Servers }

// Connected reports whether a live connection is open.
func (c *Core) Connected() bool { return c.conn != nil }

// ConnInfo reports the current connection's server name and dialect.
func (c *Core) ConnServer() string         { return c.connServer }
func (c *Core) Dialect() adapter.Dialect   { return c.dialect }

// Connect opens a connection to a configured server, resolving its password from
// the configured source, and records the connection in the current workspace.
func (c *Core) Connect(ctx context.Context, name string) error {
	srv, ok := c.servers.Servers[name]
	if !ok {
		return fmt.Errorf("no server named %q (see \\server list)", name)
	}
	ad, err := adapter.New(srv.Type)
	if err != nil {
		return err
	}
	password, err := resolvePassword(srv)
	if err != nil {
		return err
	}
	params := adapter.ConnectParams{
		Host:             srv.Host,
		Port:             srv.Port,
		User:             srv.User,
		Password:         password,
		Database:         srv.DefaultDatabase,
		ConnectionString: srv.ConnectionString,
	}
	if err := ad.Connect(ctx, params); err != nil {
		return err
	}

	if c.conn != nil {
		_ = c.conn.Disconnect()
	}
	c.conn = ad
	c.connServer = name
	c.dialect = ad.Dialect()

	c.current.CurrentServer = name
	c.current.CurrentDatabase = srv.DefaultDatabase
	_ = c.workspaces.Save(c.current)
	c.log("CONNECT", name, "database", orNone(srv.DefaultDatabase))
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// Disconnect closes any open connection.
func (c *Core) Disconnect() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Disconnect()
	c.conn, c.connServer, c.dialect = nil, "", ""
	c.current.CurrentServer = ""
	c.current.CurrentDatabase = ""
	_ = c.workspaces.Save(c.current)
	return err
}

// Use switches the current database on the live connection.
func (c *Core) Use(ctx context.Context, db string) error {
	if c.conn == nil {
		return ErrNotConnected
	}
	if err := c.conn.UseDatabase(ctx, db); err != nil {
		return err
	}
	c.current.CurrentDatabase = db
	_ = c.workspaces.Save(c.current)
	c.log("USE", "database", db)
	return nil
}

// The following delegate to the active adapter, guarding the connection.

func (c *Core) ListDatabases(ctx context.Context) ([]string, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn.ListDatabases(ctx)
}

func (c *Core) ListSchemas(ctx context.Context) ([]string, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn.ListSchemas(ctx)
}

func (c *Core) ListTables(ctx context.Context) ([]adapter.ObjectRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn.ListTables(ctx)
}

func (c *Core) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn.ListViews(ctx)
}

func (c *Core) Describe(ctx context.Context, name string) (adapter.ObjectDetail, error) {
	if c.conn == nil {
		return adapter.ObjectDetail{}, ErrNotConnected
	}
	return c.conn.DescribeObject(ctx, name)
}

// RunQuery executes a row-returning statement. The caller owns the RowStream and
// must Close it.
func (c *Core) RunQuery(ctx context.Context, sql string) (adapter.RowStream, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	rs, err := c.conn.RunQuery(ctx, sql)
	if err == nil {
		c.log("RUN", "query")
	}
	return rs, err
}

// RunStatement executes a non-row-returning statement.
func (c *Core) RunStatement(ctx context.Context, sql string) (adapter.Result, error) {
	if c.conn == nil {
		return adapter.Result{}, ErrNotConnected
	}
	res, err := c.conn.RunStatement(ctx, sql)
	if err == nil {
		c.log("RUN", "statement")
	}
	return res, err
}

// resolvePassword obtains a server's password from its configured source.
// Supported now: empty/"none" (no password) and "env:VAR". "prompt" and
// "keyring" arrive with secret handling in Phase 7.
func resolvePassword(srv config.Server) (string, error) {
	src := srv.PasswordSource
	switch {
	case src == "" || src == "none":
		return "", nil
	case strings.HasPrefix(src, "env:"):
		name := strings.TrimPrefix(src, "env:")
		if name == "" {
			return "", errors.New(`password_source "env:" is missing a variable name`)
		}
		return os.Getenv(name), nil
	case src == "prompt":
		return "", errors.New(`password_source "prompt" is not supported yet; use env:VAR`)
	case src == "keyring":
		return "", errors.New(`password_source "keyring" arrives in Phase 7; use env:VAR`)
	default:
		return "", fmt.Errorf("unknown password_source %q", src)
	}
}
