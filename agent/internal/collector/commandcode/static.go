package commandcode

import (
	"context"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

// CollectProvider collects billing state from an already-loaded provider.
// Service mode uses this path after the provider credential has been loaded
// from the platform SecretStore, so no plaintext provider config file is
// required at runtime.
func (c *Collector) CollectProvider(ctx context.Context, provider *Provider) (*domain.UsageSummary, error) {
	if err := ValidateProvider(provider, false); err != nil {
		return nil, err
	}

	creditsStatus, creditsBody, err := c.get(ctx, creditsPath, provider.APIKey)
	if err != nil {
		return nil, err
	}
	if err := raiseForStatus(creditsStatus); err != nil {
		return nil, err
	}
	creditsRoot, err := decodeObject(creditsBody, "credits")
	if err != nil {
		return nil, err
	}
	usage, err := parseCredits(creditsRoot)
	if err != nil {
		return nil, err
	}

	plan, planLimit := "", (*float64)(nil)
	if status, body, requestErr := c.get(ctx, subscriptionsPath, provider.APIKey); requestErr == nil && status >= 200 && status < 300 {
		if root, decodeErr := decodeObject(body, "subscription"); decodeErr == nil {
			plan, planLimit, _ = parsePlan(root)
		}
	}

	if plan != "" {
		usage.Plan = stringPtr(plan)
	}
	if usage.Credit != nil && planLimit != nil && usage.Credit.Remaining != nil && *usage.Credit.Remaining <= *planLimit {
		limit := *planLimit
		usage.Credit.Limit = &limit
	}
	return usage, nil
}
