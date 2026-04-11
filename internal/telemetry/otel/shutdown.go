package otel

import (
	"context"
	"errors"
	"time"
)

// Shutdown gracefully shuts down all OTEL providers and exporters.
func (p *Provider) Shutdown(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Use a timeout if the context doesn't already have one.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	var errs []error
	for _, fn := range p.shutdownFuncs {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
