package commandcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

const (
	defaultBillingBase = "https://api.commandcode.ai"
	creditsPath        = "/alpha/billing/credits"
	subscriptionsPath  = "/alpha/billing/subscriptions"
	maxResponseBytes   = 1_000_000
)

var planLabels = map[string]string{
	"individual-go":       "Go",
	"individual-goat":     "GOAT",
	"individual-pro":      "Pro",
	"individual-pro-v1":   "Pro",
	"individual-provider": "Provider",
	"individual-max":      "Max",
	"individual-ultra":    "Ultra",
	"teams-pro":           "Teams Pro",
}

var planCredits = map[string]float64{
	"individual-go":       10,
	"individual-goat":     70,
	"individual-pro":      30,
	"individual-pro-v1":   80,
	"individual-provider": 15,
	"individual-max":      150,
	"individual-ultra":    300,
	"teams-pro":           40,
}

type Collector struct {
	ConfigPaths      []string
	ProviderID       string
	AllowNonOfficial bool
	Client           *http.Client
	BillingBaseURL   string
	UserAgent        string
}

func New(configPaths []string) *Collector {
	return &Collector{
		ConfigPaths:    configPaths,
		Client:         &http.Client{Timeout: 8 * time.Second},
		BillingBaseURL: defaultBillingBase,
		UserAgent:      "ai-control-agent/0.2",
	}
}

func NewFromEnvironment() (*Collector, bool) {
	paths := DefaultConfigPaths()
	if len(paths) == 0 {
		return nil, false
	}
	providerID := strings.TrimSpace(getenv("HUD_COMMANDCODE_PROVIDER_ID"))
	_, provider, err := FindProviderAcrossConfigs(paths, providerID)
	if err != nil || provider == nil {
		return nil, false
	}
	collector := New(paths)
	collector.ProviderID = provider.ID
	collector.AllowNonOfficial = providerID != ""
	return collector, true
}

func (c *Collector) Collect(ctx context.Context) (*domain.UsageSummary, error) {
	paths := c.ConfigPaths
	if len(paths) == 0 {
		paths = DefaultConfigPaths()
	}
	_, provider, err := FindProviderAcrossConfigs(paths, c.ProviderID)
	if err != nil {
		return nil, errors.New("ZCode provider config could not be read")
	}
	if err := ValidateProvider(provider, c.AllowNonOfficial); err != nil {
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

func (c *Collector) get(ctx context.Context, path, apiKey string) (int, []byte, error) {
	base := c.BillingBaseURL
	if base == "" {
		base = defaultBillingBase
	}
	parsed, err := url.Parse(strings.TrimRight(base, "/") + path)
	if err != nil {
		return 0, nil, errors.New("CommandCode billing request failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, nil, errors.New("CommandCode billing request failed")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	userAgent := c.UserAgent
	if userAgent == "" {
		userAgent = "ai-control-agent/0.2"
	}
	req.Header.Set("User-Agent", userAgent)

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, errors.New("CommandCode billing request failed")
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return 0, nil, errors.New("CommandCode billing request failed")
	}
	if len(body) > maxResponseBytes {
		return 0, nil, errors.New("CommandCode billing response too large")
	}
	return response.StatusCode, body, nil
}

func raiseForStatus(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("CommandCode authentication failed")
	case http.StatusForbidden:
		return errors.New("CommandCode billing access denied")
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("CommandCode billing API unavailable (%d)", status)
	}
	return nil
}

func decodeObject(body []byte, label string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("Unsupported CommandCode %s response", label)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Unsupported CommandCode %s response", label)
	}
	return object, nil
}

func parseCredits(root map[string]any) (*domain.UsageSummary, error) {
	credits, ok := root["credits"].(map[string]any)
	if !ok {
		return nil, errors.New("Unsupported CommandCode credits response")
	}
	monthly, ok := number(credits["monthlyCredits"])
	if !ok || monthly < 0 {
		return nil, errors.New("Unsupported CommandCode credits response")
	}
	purchased, _ := number(credits["purchasedCredits"])
	free, _ := number(credits["freeCredits"])
	remaining := monthly + math.Max(0, purchased) + math.Max(0, free)
	unit := "USD"

	limits, _ := root["windowLimits"].(map[string]any)
	if limits == nil {
		limits, _ = credits["windowLimits"].(map[string]any)
	}
	windows := make([]domain.UsageWindow, 0, 2)
	if window := parseUsageWindow(limits, "fiveHour", "5h"); window != nil {
		windows = append(windows, *window)
	}
	if window := parseUsageWindow(limits, "weekly", "weekly"); window != nil {
		windows = append(windows, *window)
	}

	return &domain.UsageSummary{
		Credit: &domain.CreditBalance{
			Remaining: floatPtr(remaining),
			Unit:      &unit,
		},
		Windows: windows,
	}, nil
}

func parseUsageWindow(limits map[string]any, key, name string) *domain.UsageWindow {
	if limits == nil {
		return nil
	}
	value, ok := limits[key].(map[string]any)
	if !ok {
		return nil
	}
	capValue, ok := number(value["cap"])
	if !ok || capValue <= 0 {
		return nil
	}
	used, _ := number(value["used"])
	used = math.Max(0, used)
	percent := math.Min(100, (used/capValue)*100)
	return &domain.UsageWindow{
		Name:        name,
		UsedPercent: floatPtr(percent),
		ResetAt:     timestamp(value["resetAt"]),
	}
}

func parsePlan(root map[string]any) (string, *float64, error) {
	if success, exists := root["success"].(bool); exists && !success {
		return "", nil, nil
	}
	data, exists := root["data"]
	if !exists || data == nil {
		return "", nil, nil
	}
	object, ok := data.(map[string]any)
	if !ok {
		return "", nil, errors.New("Unsupported CommandCode subscription response")
	}
	planID, ok := object["planId"].(string)
	planID = strings.TrimSpace(planID)
	if !ok || planID == "" {
		return "", nil, nil
	}
	if len(planID) > 128 {
		planID = planID[:128]
	}
	label := planID
	if mapped, ok := planLabels[planID]; ok {
		label = mapped
	}
	if limit, ok := planCredits[planID]; ok {
		return label, floatPtr(limit), nil
	}
	return label, nil, nil
}

func number(value any) (float64, bool) {
	var result float64
	var err error
	switch typed := value.(type) {
	case nil:
		return 0, false
	case json.Number:
		result, err = typed.Float64()
	case float64:
		result = typed
	case float32:
		result = float64(typed)
	case int:
		result = float64(typed)
	case int64:
		result = float64(typed)
	case string:
		result, err = strconv.ParseFloat(strings.TrimSpace(typed), 64)
	case bool:
		return 0, false
	default:
		return 0, false
	}
	if err != nil || math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, false
	}
	return result, true
}

func timestamp(value any) *time.Time {
	// JSON decoding uses json.Number. Preserve integral epoch values before any
	// float64 conversion so millisecond timestamps do not acquire nanosecond
	// artifacts such as .707000017Z on large Unix values.
	if rawNumber, ok := value.(json.Number); ok {
		text := strings.TrimSpace(rawNumber.String())
		if text != "" && !strings.ContainsAny(text, ".eE") {
			if raw, err := strconv.ParseInt(text, 10, 64); err == nil && raw > 0 {
				if raw > 10_000_000_000 {
					parsed := time.UnixMilli(raw).UTC()
					return &parsed
				}
				if raw <= 253402300799 {
					parsed := time.Unix(raw, 0).UTC()
					return &parsed
				}
				return nil
			}
		}
	}

	if numeric, ok := number(value); ok && numeric > 0 {
		seconds := numeric
		if numeric > 10_000_000_000 {
			seconds = numeric / 1000
		}
		whole, fraction := math.Modf(seconds)
		if whole <= 0 || whole > 253402300799 {
			return nil
		}
		parsed := time.Unix(int64(whole), int64(math.Round(fraction*1_000_000_000))).UTC()
		return &parsed
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil
	}
	return &parsed
}

func floatPtr(value float64) *float64 { return &value }
func stringPtr(value string) *string   { return &value }

func getenv(name string) string { return strings.TrimSpace(strings.Trim(os.Getenv(name), "\x00")) }
