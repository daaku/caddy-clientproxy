package clientproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/hashicorp/yamux"
)

const shutdownTimeout = time.Minute

var yamuxConfig = &yamux.Config{
	AcceptBacklog:          256,
	EnableKeepAlive:        true,
	KeepAliveInterval:      5 * time.Minute,
	ConnectionWriteTimeout: 10 * time.Second,
	MaxStreamWindowSize:    256 * 1024,
	StreamCloseTimeout:     5 * time.Minute,
	StreamOpenTimeout:      75 * time.Second,
	LogOutput:              os.Stderr,
}

func init() {
	caddy.RegisterModule(&Middleware{})
	httpcaddyfile.RegisterHandlerDirective("client_proxy", parseCaddyfile)
}

type handler struct {
	proxy *httputil.ReverseProxy
	done  chan struct{}
}

// Middleware implements an HTTP handler that allows for a client to become the
// reverse proxy.
type Middleware struct {
	// The secret to allow for registering a client.
	Secret string `json:"secret,omitempty"`

	// stores a *handler, when available
	handler atomic.Value
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
	defer conn.Close() // backup close
	if err := buf.Flush(); err != nil {
		return fmt.Errorf("client_proxy: unexpected flush error: %w", err)
	}
	if buf.Reader.Buffered() > 0 {
		conn = &bufConn{Conn: conn, Reader: buf.Reader}
	}
	yamuxClient, err := yamux.Client(conn, yamuxConfig)
	if err != nil {
		return fmt.Errorf("client_proxy: unable to create yamux.Client: %w", err)
	}

	// close the old one, if one is there
	if handler, ok := m.handler.Load().(*handler); ok {
		close(handler.done)
	}

	done := make(chan struct{})
	m.handler.Store(&handler{
		done: done,
		proxy: &httputil.ReverseProxy{
			Transport: &http.Transport{
				DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return yamuxClient.Open()
				},
			},
			Director: func(r *http.Request) {
				r.URL.Scheme = "https"
				r.URL.Host = r.Host
			},
		},
	})
	<-done // wait until we're being replaced
	if err := yamuxClient.Close(); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return fmt.Errorf("client_proxy: error shutting down yamux.Client: %w", err)
	}
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.Header.Get("X-Client-Proxy") == m.Secret {
		return m.acceptProxy(w, r)
	}
	if handler, ok := m.handler.Load().(*handler); ok {
		handler.proxy.ServeHTTP(w, r)
		return nil
	}
	return next.ServeHTTP(w, r)
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
