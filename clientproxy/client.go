// Package clientproxy provides a method to dial into a Caddy server and use
// this process to serve HTTP requests.
package clientproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	urlp "net/url"

	"golang.org/x/net/http2"
)

// DialAndServe connects to the given URL and serves the provided handler. The
// URL must contain the scheme, and the path must be correctly set to the
// secret expected by the server.
func DialAndServe(ctx context.Context, url string, h http.Handler) error {
	u, err := urlp.Parse(url)
	if err != nil {
		return err
	}
	var conn net.Conn
	addr := u.Host
	if u.Scheme == "https" {
		if u.Port() == "" {
			addr += ":443"
		}
		dialer := tls.Dialer{Config: &tls.Config{ServerName: u.Hostname()}}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	} else {
		if u.Port() == "" {
			addr += ":80"
		}
		dialer := net.Dialer{}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("clientproxy: DialAndServe: %w", err)
	}
	defer conn.Close() // defensive close, ServeConn will handle this for us
	var b bytes.Buffer
	b.WriteString("GET ")
	b.WriteString(u.RequestURI())
	b.WriteString(" HTTP/1.1\r\nHost: ")
	b.WriteString(u.Hostname())
	b.WriteString("\r\n\r\n")
	if _, err := conn.Write(b.Bytes()); err != nil {
		return err
	}
	h2s := &http2.Server{}
	h2s.ServeConn(conn, &http2.ServeConnOpts{
		Context: ctx,
		Handler: h,
	})
	return nil
}
