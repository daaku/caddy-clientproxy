// Package clientproxy provide the Caddy clientproxy module.
package clientproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/hashicorp/yamux"
)

const (
	appID     = "client_proxy_app"
	sRegister = "client_proxy_register"
	sDispatch = "client_proxy_dispatch"
)

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
	caddy.RegisterModule(&App{})
	caddy.RegisterModule(&Register{})
	caddy.RegisterModule(&Dispatch{})
	httpcaddyfile.RegisterHandlerDirective(sRegister, parseCaddyfileRegister)
	httpcaddyfile.RegisterHandlerDirective(sDispatch, parseCaddyfileDispatch)
	httpcaddyfile.RegisterDirectiveOrder(sDispatch, httpcaddyfile.Before, "respond")
	httpcaddyfile.RegisterDirectiveOrder(sRegister, httpcaddyfile.Before, "respond")
}

// App stores the shared handlers that have been registered.
// This is how Register and Dispatch share state.
type App struct {
	app sync.Map
}

// CaddyModule returns the Caddy module information.
func (*App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  appID,
		New: func() caddy.Module { return new(App) },
	}
}

// Start and Stop causes us to implement `caddy.App` and ensures our instance
// gets cached on `ctx.App` use.
func (g *App) Start() error {
	return nil
}

func (g *App) Stop() error {
	return nil
}

func (g *App) setHandler(name string, rp *httputil.ReverseProxy) *handler {
	h := &handler{
		done:  make(chan struct{}),
		proxy: rp,
	}
	if old, found := g.app.Swap(name, h); found {
		close(old.(*handler).done)
	}
	return h
}

func (g *App) getHandler(name string) *handler {
	if h, found := g.app.Load(name); found {
		return h.(*handler)
	}
	return nil
}

type handler struct {
	proxy *httputil.ReverseProxy
	done  chan struct{}
}

// Register allows for client proxies to register themselves
// to become available to the ClientProxy handler.
type Register struct {
	// The secret to allow for registering a client.
	Secret string `json:"secret,omitempty"`
	app    *App
}

// CaddyModule returns the Caddy module information.
func (*Register) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.client_proxy_register",
		New: func() caddy.Module { return new(Register) },
	}
}

// Provision implements caddy.Provisioner.
func (m *Register) Provision(ctx caddy.Context) error {
	app, err := ctx.App(appID)
	if err != nil {
		return fmt.Errorf("error provisioning %s: %w", appID, err)
	}
	m.app = app.(*App)
	return nil
}

// Validate implements caddy.Validator.
func (m *Register) Validate() error {
	if m.Secret == "" {
		return fmt.Errorf("no secret")
	}
	return nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (m *Register) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name

	// require an argument
	if !d.NextArg() {
		return d.ArgErr()
	}

	// store the argument
	m.Secret = d.Val()
	return nil
}

func (m *Register) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.Header.Get("X-Client-Proxy-Secret") != m.Secret {
		log.Println("ignoring request to register client proxy without secret")
		return next.ServeHTTP(w, r)
	}
	name := r.Header.Get("X-Client-Proxy-Name")
	if name == "" {
		log.Println("ignoring request to register client proxy without name")
		return next.ServeHTTP(w, r)
	}

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
	rp := &httputil.ReverseProxy{
		Transport: &http.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return yamuxClient.Open()
			},
		},
		Director: func(r *http.Request) {
			r.URL.Scheme = "https"
			r.URL.Host = r.Host
		},
	}
	h := m.app.setHandler(name, rp)
	<-h.done // wait until we're being replaced
	if err := yamuxClient.Close(); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return fmt.Errorf("client_proxy: error shutting down yamux.Client: %w", err)
	}
	return nil
}

// parseCaddyfileRegister unmarshals tokens from h into a new Middleware.
func parseCaddyfileRegister(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Register
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return &m, err
}

// Dispatch implements an HTTP handler that allows for a client to become the
// reverse proxy.
type Dispatch struct {
	// The named ClientProxy to dispatch to, if available.
	Name string `json:"name"`
	app  *App
}

// CaddyModule returns the Caddy module information.
func (*Dispatch) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.client_proxy_dispatch",
		New: func() caddy.Module { return new(Dispatch) },
	}
}

// Provision implements caddy.Provisioner.
func (m *Dispatch) Provision(ctx caddy.Context) error {
	app, err := ctx.App(appID)
	if err != nil {
		return fmt.Errorf("error provisioning %s: %w", appID, err)
	}
	m.app = app.(*App)
	return nil
}

// Validate implements caddy.Validator.
func (m *Dispatch) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("no name")
	}
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (m *Dispatch) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if h := m.app.getHandler(m.Name); h != nil {
		h.proxy.ServeHTTP(w, r)
		return nil
	}
	return next.ServeHTTP(w, r)
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (m *Dispatch) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name

	// require an argument
	if !d.NextArg() {
		return d.ArgErr()
	}

	// store the argument
	m.Name = d.Val()
	return nil
}

// parseCaddyfileDispatch unmarshals tokens from h into a new Middleware.
func parseCaddyfileDispatch(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Dispatch
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
	_ caddy.Provisioner           = (*Register)(nil)
	_ caddy.Validator             = (*Register)(nil)
	_ caddyhttp.MiddlewareHandler = (*Register)(nil)
	_ caddyfile.Unmarshaler       = (*Register)(nil)
	_ caddy.Provisioner           = (*Dispatch)(nil)
	_ caddy.Validator             = (*Dispatch)(nil)
	_ caddyhttp.MiddlewareHandler = (*Dispatch)(nil)
	_ caddyfile.Unmarshaler       = (*Dispatch)(nil)
)
