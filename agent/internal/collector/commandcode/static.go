package commandcode

import "context"

// CollectProvider collects billing state from an already-loaded provider.
// Service mode uses this path after the provider credential has been loaded
// from the platform SecretStore, so no plaintext provider config file is
// required at runtime.
func (c *Collector) CollectProvider(ctx context.Context, provider *Provider) (*domainUsageSummary, error) {
	return c.collectProvider(ctx, provider)
}
