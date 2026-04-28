package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/config"
	skerrors "github.com/jaimegago/skillhub/internal/errors"
	"github.com/jaimegago/skillhub/internal/marketplace"
)

// SearchPluginsInput is the typed input for the search_plugins tool.
type SearchPluginsInput struct {
	Query       string `json:"query"                jsonschema:"Case-insensitive substring to match against plugin name, description, keywords, and category"`
	Marketplace string `json:"marketplace,omitempty" jsonschema:"Restrict search to a single marketplace by name; omit to search all configured marketplaces"`
	Limit       int    `json:"limit,omitempty"      jsonschema:"Maximum number of results to return; defaults to 50 when unset or 0"`
}

// SearchPluginsOutput is the typed output for the search_plugins tool.
type SearchPluginsOutput struct {
	Plugins   []AvailablePlugin         `json:"plugins"`
	Sources   []MarketplaceSourceStatus `json:"sources"`
	Truncated bool                      `json:"truncated"`
	Total     int                       `json:"total"`
	Query     string                    `json:"query"`
}

// NewSearchPlugins returns the search_plugins tool declaration.
func NewSearchPlugins() Tool {
	return Tool{
		Name:        "search_plugins",
		Description: "Perform a case-insensitive substring match across plugin names and descriptions in all configured marketplace sources. Returns matching plugins with name, description, and version.",
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "search_plugins",
				Description: "Perform a case-insensitive substring match across plugin names and descriptions in all configured marketplace sources. Returns matching plugins with name, description, and version.",
			}, HandleSearchPlugins)
		},
	}
}

// HandleSearchPlugins is the generic typed handler for search_plugins.
func HandleSearchPlugins(ctx context.Context, _ *mcp.CallToolRequest, input SearchPluginsInput) (*mcp.CallToolResult, SearchPluginsOutput, error) {
	if strings.TrimSpace(input.Query) == "" {
		return errResult(skerrors.ErrInvalidInput, "query is required", ""), SearchPluginsOutput{}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return errResult(skerrors.ErrMarketplaceUnreachable, "failed to load config", err.Error()), SearchPluginsOutput{}, nil
	}

	needle := strings.ToLower(input.Query)

	out := SearchPluginsOutput{
		Plugins: []AvailablePlugin{},
		Sources: []MarketplaceSourceStatus{},
		Query:   input.Query,
	}

	cacheDir := config.CacheDir()

	for _, src := range cfg.MarketplaceSources {
		res := marketplace.Fetch(ctx, src, cacheDir, false)

		status := MarketplaceSourceStatus{
			URL:    src.URL,
			Cached: res.Cached,
			Error:  res.Error,
		}

		if res.Index == nil {
			out.Sources = append(out.Sources, status)
			continue
		}

		status.Name = res.Index.Name

		if input.Marketplace != "" && res.Index.Name != input.Marketplace {
			continue
		}

		status.Count = len(res.Index.Plugins)
		out.Sources = append(out.Sources, status)

		for _, p := range res.Index.Plugins {
			if !pluginMatchesQuery(p, needle) {
				continue
			}
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

// pluginMatchesQuery reports whether p contains needle in any searchable text field.
func pluginMatchesQuery(p marketplace.PluginEntry, needle string) bool {
	if strings.Contains(strings.ToLower(p.Name), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Description), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Category), needle) {
		return true
	}
	for _, kw := range p.Keywords {
		if strings.Contains(strings.ToLower(kw), needle) {
			return true
		}
	}
	return false
}
