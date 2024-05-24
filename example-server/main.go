package main

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"golang.org/x/net/http2"
)

func main() {
	conn, err := tls.Dial("tcp", "localhost:4430", &tls.Config{
		ServerName: "localhost",
	})
	if err != nil {
		panic(err)
	}
	payload := []byte("GET /this_is_the_secret_path HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if _, err := conn.Write(payload); err != nil {
		panic(err)
	}
	h2s := &http2.Server{
		CountError: func(errType string) {
			println("count error", errType)
		},
	}
	h2s.ServeConn(conn, &http2.ServeConnOpts{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "hello from the other side")
		}),
	})
}
