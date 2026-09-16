package tunnel

import "context"

// externalProvider is a Provider that spawns no subprocess. It trusts a URL
// the caller already knows is publicly reachable and routed back to the
// local port some other way — e.g. an SSH reverse tunnel into a
// self-hosted reverse proxy. No reachability check is performed; a wrong
// URL simply causes whatever webhook create/update call uses it to fail the
// same way it would for any other invalid URL.
type externalProvider struct {
	publicURL string
	ctx       context.Context
}

// NewExternalProvider returns a Provider that always reports publicURL as
// the externally reachable URL, without spawning any tunnel process.
func NewExternalProvider(publicURL string) Provider {
	return &externalProvider{publicURL: publicURL}
}

func (p *externalProvider) Start(ctx context.Context, _ int) (string, error) {
	p.ctx = ctx
	return p.publicURL, nil
}

// Wait blocks until the context passed to Start is done, since there is no
// subprocess of its own to wait on.
func (p *externalProvider) Wait() error {
	<-p.ctx.Done()
	return p.ctx.Err()
}
