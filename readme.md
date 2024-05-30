# caddy-clientproxy

This [Caddy](https://caddyserver.com/) module provides a handler that proxies
traffic from your Caddy server to your origin. Using the sibling
[clientproxy](https://github.com/daaku/clientproxy) package, your origin
initiates and maintains a connection to your Caddy server. Caddy uses this
connection to proxy requests to your origin. This way your origin does not need
to accept any connections, and need only support outbound connections.

# Usage

1. Make sure you're using `https` as appropriate.
1. Use a sufficiently large shared secret.
1. Order the handlers correctly. This is a _terminal_ handler, in that it does
   not continue the chain if the reverse proxy is available.
1. Use [clientproxy](https://github.com/daaku/clientproxy) to make your
   server(s) available via your caddy.

# Limitations

1. A single TCP connection is used to connect to the backend.
1. Only one active backend server is supported.
1. Connection upgrades like WebSockets are not supported.

# Configuration

You'll need to [order](https://caddyserver.com/docs/caddyfile/options#order)
this handler, or use
[route](https://caddyserver.com/docs/caddyfile/directives/route):

```
{
	order client_proxy before respond
}

example.com {
	client_proxy 46f20973162c43d09bf7ca2311a9c3ca
}
```

# clientproxy

On the machine which hosts your origin server, you'll need to run
[clientproxy](https://github.com/daaku/clientproxy). This process will maintain
a connection to your Caddy instance, and accept and proxy requests to your
origin server. You'll need a configuration file:

```toml
[[proxy]]
register = "https://example.com/46f20973162c43d09bf7ca2311a9c3ca"
forward = "http://localhost:8080"
```

Run the `clientproxy` server:

```bash
clientproxy config.toml
```

Now a request to `https://example.com` should get proxied to your origin server.

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

In terminal 2, start the
[example server](https://github.com/daaku/clientproxy/tree/main/cmd/example-server).
This is actually the process that handles the HTTP requests, but it does not
listen on any ports.

```
cd clientproxy
go run ./cmd/example-server
```

In terminal 3, make a request using `curl` to your caddy server:

```
curl -k https://localhost:4430/
```
