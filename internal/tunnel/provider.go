package tunnel

import (
	"context"
	"time"
)

// Provider exposes a local port at a URL reachable from the public
// internet, so a Mollie webhook subscription can point at it.
type Provider interface {
	// Start begins exposing localhost:port publicly and returns the
	// externally reachable URL once ready.
	Start(ctx context.Context, port int) (url string, err error)
	// Wait blocks until the tunnel's underlying subprocess (if any) exits,
	// or until ctx is done for providers with no subprocess of their own.
	Wait() error
}

// cloudflaredProvider adapts Start/Tunnel to the Provider interface.
type cloudflaredProvider struct {
	path        string
	waitTimeout time.Duration
	tun         *Tunnel
}

// NewCloudflaredProvider returns a Provider that spawns a cloudflared quick
// tunnel pointed at the given local port. cloudflaredPath is the path to the
// cloudflared binary (e.g. from exec.LookPath); waitTimeout bounds how long
// to wait for cloudflared to report its public URL.
func NewCloudflaredProvider(cloudflaredPath string, waitTimeout time.Duration) Provider {
	return &cloudflaredProvider{path: cloudflaredPath, waitTimeout: waitTimeout}
}

func (p *cloudflaredProvider) Start(ctx context.Context, port int) (string, error) {
	tun, err := Start(ctx, p.path, port, p.waitTimeout)
	if err != nil {
		return "", err
	}
	p.tun = tun
	return tun.URL, nil
}

func (p *cloudflaredProvider) Wait() error {
	return p.tun.Wait()
}
