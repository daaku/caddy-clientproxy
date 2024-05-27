package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/daaku/caddy-client-proxy/clientproxy"
)

func main() {
	err := clientproxy.DialAndServe(
		context.Background(),
		"https://localhost:4430/this_is_the_secret_path",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "hello from the other side")
		}),
	)
	if err != nil {
		panic(err)
	}
}