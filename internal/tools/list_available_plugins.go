package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/config"
	skerrors "github.com/jaimegago/skillhub/internal/errors"
	"github.com/jaimegago/skillhub/internal/marketplace"
)

// ListAvailablePluginsInput is the typed input for the list_available_plugins tool.
type ListAvailablePluginsInput struct {
	Marketplace string `json:"marketplace,omitempty" jsonschema:"Filter results to a single marketplace by name; omit to list from all configured marketplaces"`
	Refresh     bool   `json:"refresh,omitempty"    jsonschema:"Force re-fetch from source, ignoring the local cache"`
	Limit       int    `json:"limit,omitempty"      jsonschema:"Maximum number of plugins to return; defaults to 50 when unset or 0; pass a larger explicit value for bulk operations — there is no unlimited sentinel"`
}

// ListAvailablePluginsOutput is the typed output for the list_available_plugins tool.
type ListAvailablePluginsOutput struct {
	Plugins   []AvailablePlugin         `json:"plugins"`
	Sources   []MarketplaceSourceStatus `json:"sources"`
	Truncated bool                      `json:"truncated"`
	Total     int                       `json:"total"`
}

// AvailablePlugin is one plugin entry in the list output.
type AvailablePlugin struct {
	Name        string   `json:"name"`
	Marketplace string   `json:"marketplace"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Category    string   `json:"category,omitempty"`
}

// MarketplaceSourceStatus reports fetch outcome for one configured source.
type MarketplaceSourceStatus struct {
	URL    string `json:"url"`
	Name   string `json:"name,omitempty"`
	Count  int    `json:"count"`
	Cached bool   `json:"cached"`
	Error  string `json:"error,omitempty"`
}

// NewListAvailablePlugins returns the list_available_plugins tool declaration.
func NewListAvailablePlugins() Tool {
	return Tool{
		Name:        "list_available_plugins",
		Description: "Read all configured marketplace sources and return available plugins with name, description, version, and source metadata.",
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "list_available_plugins",
				Description: "Read all configured marketplace sources and return available plugins with name, description, version, and source metadata.",
			}, HandleListAvailablePlugins)
		},
	}
}

// HandleListAvailablePlugins is the generic typed handler for list_available_plugins.
// Partial marketplace failures are reported in Sources[i].Error; the handler
// never returns a top-level error for per-source failures.
func HandleListAvailablePlugins(ctx context.Context, _ *mcp.CallToolRequest, input ListAvailablePluginsInput) (*mcp.CallToolResult, ListAvailablePluginsOutput, error) {
	cfg, err := config.Load()
	if err != nil {
		return errResult(skerrors.ErrMarketplaceUnreachable, "failed to load config", err.Error()), ListAvailablePluginsOutput{}, nil
	}

	out := ListAvailablePluginsOutput{
		Plugins: []AvailablePlugin{},
		Sources: []MarketplaceSourceStatus{},
	}

	cacheDir := config.CacheDir()

	for _, src := range cfg.MarketplaceSources {
		res := marketplace.Fetch(ctx, src, cacheDir, input.Refresh)

		status := MarketplaceSourceStatus{
			URL:    src.URL,
			Cached: res.Cached,
			Error:  res.Error,
		}

		if res.Index == nil {
			// Fetch failed entirely; include error entry regardless of filter.
			out.Sources = append(out.Sources, status)
			continue
		}

		status.Name = res.Index.Name

		// Apply marketplace name filter after we know the index name.
		if input.Marketplace != "" && res.Index.Name != input.Marketplace {
			continue
		}

		status.Count = len(res.Index.Plugins)
		out.Sources = append(out.Sources, status)

		for _, p := range res.Index.Plugins {
			ap := AvailablePlugin{
				Name:        p.Name,
				Marketplace: res.Index.Name,
				Version:     p.Version,
				Description: p.Description,
				Homepage:    p.Homepage,
				Category:    p.Category,
				Keywords:    p.Keywords,
			}
			if p.Author != nil {
				ap.Author = p.Author.Name
			}
			out.Plugins = append(out.Plugins, ap)
		}
	}

	const defaultLimit = 50
	limit := input.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	out.Total = len(out.Plugins)
	out.Truncated = out.Total > limit
	if out.Truncated {
		out.Plugins = out.Plugins[:limit]
	}

	return nil, out, nil
}
