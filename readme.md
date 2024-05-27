# caddy-client-proxy

**Proof of concept. Work in progress.**

This Caddy module provides a handler that allows for a client connection to
be turned into a server. This way your backend server connects to your Caddy
load balancer instance, and the requests are then sent over this connection. It
allows for your backend server to not accept any public connections, and only
requires it to support outgoing connections.

# Implementation

In Caddy, when the module recieves a valid client request that intends to
become the server, it Hijacks the connection, and converts it to a HTTP2 Client
Connection, which can be used as a `http.RoundTripper`. This serves as the
reverse proxy target.

The server makes a TLS secured HTTP/1.1 connection to Caddy, and then treats
that connection as a HTTP2 Server Connection. It then starts serving requests on
this connection.

# Testing

In terminal 1, start the caddy server with the sample Caddyfile:

```
xcaddy run -c Caddyfile
```

In terminal 2, start the sample server. This is actually the process that
handles the HTTP requests, but it does not listen on any ports.

```
cd clientproxy
go run ./cmd/example-server
```

In terminal 3, make a request using `curl` to your caddy server:

```
curl -k https://localhost:4430/
```
