package clientproxy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"golang.org/x/net/http2"
)

func init() {
	caddy.RegisterModule(&Middleware{})
	httpcaddyfile.RegisterHandlerDirective("client_proxy", parseCaddyfile)
}

var h2t = http2.Transport{
	// TODO: this enables health checks. make optional/configurable. how to use?
	ReadIdleTimeout: time.Minute,
}

// Middleware implements an HTTP handler that allows for a client to become the
// reverse proxy.
type Middleware struct {
	// The secret to allow for registering a client.
	Secret string `json:"secret,omitempty"`

	handler http.Handler
}

// CaddyModule returns the Caddy module information.
func (*Middleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.client_proxy",
		New: func() caddy.Module { return new(Middleware) },
	}
}

// Provision implements caddy.Provisioner.
func (m *Middleware) Provision(ctx caddy.Context) error {
	return nil
}

// Validate implements caddy.Validator.
func (m *Middleware) Validate() error {
	if m.Secret == "" {
		return fmt.Errorf("no secret")
	}
	return nil
}

func (m *Middleware) acceptProxy(w http.ResponseWriter, r *http.Request) error {
	rc := http.NewResponseController(w)
	if err := rc.EnableFullDuplex(); err != nil {
		return fmt.Errorf("client_proxy: must connect using HTTP/1.1: %w", err)
	}
	conn, buf, err := rc.Hijack()
	if err != nil {
		return fmt.Errorf("client_proxy: must connect using HTTP/1.1: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			// TODO hook up logger
			println("client_proxy: error closing hijacked conn:", err)
		}
	}()
	if err := buf.Flush(); err != nil {
		return fmt.Errorf("client_proxy: unexpected flush error: %w", err)
	}
	if buf.Reader.Buffered() > 0 {
		// TODO: figure out a way to make the initial request sized so that we don't
		// need to wrap the net.Conn. it will probably let magical interfaces on
		// net.Conn allow to be used?
		conn = &bufConn{Conn: conn, Reader: buf.Reader}
	}
	h2conn, err := h2t.NewClientConn(conn)
	if err != nil {
		return fmt.Errorf("client_proxy: unable to create ClientConn: %w", err)
	}
	// TODO: mutex guard cleanup etc
	m.handler = &httputil.ReverseProxy{
		Transport: h2conn,
		Director: func(r *http.Request) {
			r.URL.Scheme = "https"
		},
	}
	select {}
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.URL.Path[1:] == m.Secret {
		return m.acceptProxy(w, r)
	}
	if m.handler == nil {
		return next.ServeHTTP(w, r)
	}
	m.handler.ServeHTTP(w, r)
	return nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (m *Middleware) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name

	// require an argument
	if !d.NextArg() {
		return d.ArgErr()
	}

	// store the argument
	m.Secret = d.Val()
	return nil
}

// parseCaddyfile unmarshals tokens from h into a new Middleware.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Middleware
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return &m, err
}

type bufConn struct {
	net.Conn
	*bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) {
	if c.Reader == nil {
		return c.Conn.Read(p)
	}
	n := c.Buffered()
	if n == 0 {
		c.Reader = nil
		return c.Conn.Read(p)
	}
	if n < len(p) {
		p = p[:n]
	}
	return c.Reader.Read(p)
}

// Interface guards
var (
	_ caddy.Provisioner           = (*Middleware)(nil)
	_ caddy.Validator             = (*Middleware)(nil)
	_ caddyhttp.MiddlewareHandler = (*Middleware)(nil)
	_ caddyfile.Unmarshaler       = (*Middleware)(nil)
)
