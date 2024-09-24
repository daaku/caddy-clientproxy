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
   origin(s) available via your caddy.

# Limitations

1. A single TCP connection is used to connect to the origin.
1. Only one active origin is supported.
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

On the machine which hosts your origin, you'll need to run
[clientproxy](https://github.com/daaku/clientproxy). This process will maintain
a connection to your Caddy instance, and accept and proxy requests to your
origin. You'll need a configuration file:

```toml
[[proxy]]
register = "https://example.com/46f20973162c43d09bf7ca2311a9c3ca"
forward = "http://localhost:8080"
```

Run the `clientproxy` daemon:

```bash
clientproxy config.toml
```

Now a request to `https://example.com` should get proxied to your origin.

# Implementation

In Caddy, when the module recieves a valid client request that intends to
become the origin, it Hijacks the connection, and uses
[yamux](https://github.com/hashicorp/yamux) to make the client the server.
This serves as the reverse proxy target.

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
