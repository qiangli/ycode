// doorrelay gives a contained benchmark call one path to the host's model
// door, restricted to one sticky binding, without the door token ever
// entering the container. Benchmark-only: the bench-container wrapper (the
// @contain provider: custom for the SWE-bench runner) uses it.
//
//	doorrelay serve SOCK STICKY   host side: a filtering proxy on the unix
//	                              socket SOCK; forwards only inference and
//	                              model listing on /sticky/STICKY/ to the door
//	                              (BENCH_DOOR: base URL, /k/<token> form)
//	doorrelay relay SOCK          container side: 127.0.0.1:24556 -> SOCK
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

const doorPort = 24556

func main() {
	if len(os.Args) == 4 && os.Args[1] == "serve" {
		fail(serve(os.Args[2], os.Args[3]))
	}
	if len(os.Args) == 3 && os.Args[1] == "relay" {
		fail(relay(os.Args[2]))
	}
	fmt.Fprintln(os.Stderr, "usage: doorrelay serve SOCK STICKY | doorrelay relay SOCK")
	os.Exit(2)
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "doorrelay:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// allowed reports whether a request may pass, and the path after the
// binding: only the one binding, only inference and model listing.
func allowed(method, path, sticky string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/sticky/"+sticky+"/")
	if !ok {
		return "", false
	}
	rest = "/" + rest
	switch strings.TrimPrefix(rest, "/openai") {
	case "/v1/chat/completions", "/v1/completions", "/v1/embeddings", "/v1/messages", "/anthropic/v1/messages":
		return rest, method == http.MethodPost
	case "/v1/models", "/health":
		return rest, method == http.MethodGet
	}
	return "", false
}

type restKey struct{}

func proxy(up *url.URL, sticky string) http.Handler {
	rp := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = up.Scheme, up.Host, up.Host
			pr.Out.URL.Path = up.Path + "/sticky/" + sticky + pr.In.Context().Value(restKey{}).(string)
			pr.Out.URL.RawPath = ""
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("x-api-key")
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := allowed(r.Method, r.URL.Path, sticky)
		if !ok {
			http.Error(w, fmt.Sprintf("doorrelay: only the sticky binding %q (inference, model listing): %s %s refused", sticky, r.Method, r.URL.Path), http.StatusForbidden)
			return
		}
		rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), restKey{}, rest)))
	})
}

func serve(sock, sticky string) error {
	raw := strings.TrimRight(os.Getenv("BENCH_DOOR"), "/")
	up, err := url.Parse(raw)
	if raw == "" || err != nil || up.Host == "" {
		return fmt.Errorf("BENCH_DOOR must be the door's base URL (http://127.0.0.1:24556/k/<token>)")
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(raw + "/health")
	if err != nil {
		return fmt.Errorf("the model door is not reachable at %s: %v", up.Host, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the model door at %s answered %s", up.Host, resp.Status)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	return (&http.Server{Handler: proxy(up, sticky), ReadHeaderTimeout: 30 * time.Second}).Serve(ln)
}

func relay(sock string) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", doorPort))
	if err != nil {
		return err
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			u, err := net.Dial("unix", sock)
			if err != nil {
				return
			}
			defer u.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(u, c); closeWrite(u); done <- struct{}{} }()
			go func() { _, _ = io.Copy(c, u); closeWrite(c); done <- struct{}{} }()
			<-done
			<-done
		}(c)
	}
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}
