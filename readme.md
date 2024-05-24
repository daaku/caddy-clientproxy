# caddy-client-proxy

**Proof of concept. Work in progress.**

This Caddy module provides a handler that allows for a client connection to
be turned into a server. This way your backend server connects to your Caddy
load balancer instance, and the requests are then sent over this connection. It
allows for your backend server to not accept any public connections, and only
requires it to support outgoing connections.

# Testing

In terminal 1, start the caddy server with the sample Caddyfile:

```
xcaddy run -c Caddyfile
```

In terminal 2, start the sample server. This is actually the process that
handles the HTTP requests, but it does not listen on any ports.

```
go run ./example-server
```

_Tip: for debugging `GODEBUG=http2debug=2 go run ./example-server` may be helpful._

In terminal 3, make a request using `curl` to your caddy server:

```
curl -k https://localhost:4430/
```
