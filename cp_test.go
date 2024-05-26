package clientproxy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/daaku/ensure"
)

// normal request
// large request
// no client registered
// second client registered
//

const secret = "the_secret"

func newMiddleware(t testing.TB) *Middleware {
	return &Middleware{Secret: secret}
}

func TestNoHandler(t *testing.T) {
	m := newMiddleware(t)
	called := false
	gw := httptest.NewRecorder()
	gr := httptest.NewRequest(http.MethodGet, "/", nil)
	ge := errors.New("an error")
	err := m.ServeHTTP(gw, gr, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		ensure.DeepEqual(t, w, gw)
		ensure.DeepEqual(t, r, gr)
		called = true
		return ge
	}))
	ensure.DeepEqual(t, err, ge)
	ensure.True(t, called)
}
