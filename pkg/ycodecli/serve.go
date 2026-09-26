package ycodecli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/qiangli/ycode/internal/harness/frontend"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
)

func (a *harnessApplication) Serve(ctx context.Context) error {
	return a.serve(ctx, defaultServeDependencies())
}

// ServeRoute serves what a declared serve command names: every network
// frontend, or with scope: frontend only its frontendRef.
func (a *harnessApplication) ServeRoute(ctx context.Context, route harnessspec.CLIDispatch, announce io.Writer) error {
	dependencies := defaultServeDependencies()
	dependencies.started = func(ref, address string) { a.announce(announce, ref, address) }
	if route.Scope == "frontend" {
		dependencies.only = route.FrontendRef
	}
	return a.serve(ctx, dependencies)
}

// announce tells the operator where a frontend listens. A UI frontend's line
// is the URL to open: the bearer token rides in the fragment, which browsers
// never send to the server.
func (a *harnessApplication) announce(out io.Writer, ref, address string) {
	configured := a.doc.Spec.Frontends[ref]
	switch {
	case configured.Kind == "nats":
		fmt.Fprintf(out, "%s: subscribed at %s\n", ref, address)
	case configured.UI != "" && configured.Auth != nil:
		fmt.Fprintf(out, "%s: open http://%s/#token=%s\n", ref, address, url.QueryEscape(os.Getenv(configured.Auth.SecretRef.Name)))
	case configured.Kind == "websocket":
		fmt.Fprintf(out, "%s: listening on ws://%s/\n", ref, address)
	default:
		fmt.Fprintf(out, "%s: listening on http://%s/\n", ref, address)
	}
}

type natsConnection interface {
	frontend.NATSConnection
	Close()
}

type serveDependencies struct {
	listen      func(network, address string) (net.Listener, error)
	connectNATS func(endpoint string) (natsConnection, error)
	started     func(ref, address string)
	only        string // serve just this frontend ref when set
}

type liveNATSConnection struct {
	*frontend.GoNATSConnection
	raw *gonats.Conn
}

func (c *liveNATSConnection) Close() { c.raw.Close() }

func defaultServeDependencies() serveDependencies {
	return serveDependencies{
		listen: net.Listen,
		connectNATS: func(endpoint string) (natsConnection, error) {
			raw, err := gonats.Connect(endpoint)
			if err != nil {
				return nil, err
			}
			wrapped, err := frontend.WrapNATSConnection(raw)
			if err != nil {
				raw.Close()
				return nil, err
			}
			return &liveNATSConnection{GoNATSConnection: wrapped, raw: raw}, nil
		},
		started: func(string, string) {},
	}
}

func (a *harnessApplication) serve(ctx context.Context, dependencies serveDependencies) error {
	if dependencies.listen == nil || dependencies.connectNATS == nil || dependencies.started == nil {
		return errors.New("serve dependencies are incomplete")
	}
	refs := make([]string, 0, len(a.doc.Spec.Frontends))
	for ref := range a.doc.Spec.Frontends {
		if dependencies.only == "" || ref == dependencies.only {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)

	var servers []*http.Server
	var listeners []net.Listener
	var subscriptions []frontend.NATSSubscription
	var connections []natsConnection
	errCh := make(chan error, len(refs))
	for _, ref := range refs {
		configured := a.doc.Spec.Frontends[ref]
		switch configured.Kind {
		case "http", "websocket":
			network, err := frontend.NewNetwork(a.doc, ref, a, envAuthenticator{})
			if err != nil {
				return err
			}
			listener, err := dependencies.listen(configured.Listen.Network, configured.Listen.Address)
			if err != nil {
				return fmt.Errorf("frontend %s listen: %w", ref, err)
			}
			server := &http.Server{Handler: network, ReadHeaderTimeout: 10 * time.Second}
			servers = append(servers, server)
			listeners = append(listeners, listener)
			dependencies.started(ref, listener.Addr().String())
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
			}()
		case "nats":
			connection, err := dependencies.connectNATS(configured.Endpoint)
			if err != nil {
				return fmt.Errorf("frontend %s connect: %w", ref, err)
			}
			network, err := frontend.NewNetwork(a.doc, ref, a, envAuthenticator{})
			if err != nil {
				connection.Close()
				return err
			}
			subscriber, err := frontend.NewNATSSubscriber(network, connection)
			if err != nil {
				connection.Close()
				return err
			}
			subscription, err := subscriber.Subscribe(ctx)
			if err != nil {
				connection.Close()
				return err
			}
			connections = append(connections, connection)
			subscriptions = append(subscriptions, subscription)
			dependencies.started(ref, configured.Endpoint)
		}
	}
	if len(servers) == 0 && len(subscriptions) == 0 {
		return errors.New("agent.yaml declares no network frontends")
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wait sync.WaitGroup
	for _, server := range servers {
		wait.Add(1)
		go func() { defer wait.Done(); _ = server.Shutdown(shutdown) }()
	}
	wait.Wait()
	for _, subscription := range subscriptions {
		_ = subscription.Unsubscribe()
	}
	for _, connection := range connections {
		connection.Close()
	}
	for _, listener := range listeners {
		_ = listener.Close()
	}
	return nil
}
