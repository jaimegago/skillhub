package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/config"
	skerrors "github.com/jaimegago/skillhub/internal/errors"
	"github.com/jaimegago/skillhub/internal/marketplace"
)

// RecommendPluginsInput is the typed input for the recommend_plugins tool.
type RecommendPluginsInput struct {
	Context     string `json:"context"              jsonschema:"Free-text description of the task or need to match plugins against"`
	Marketplace string `json:"marketplace,omitempty" jsonschema:"Restrict recommendations to a single marketplace by name; omit to search all configured marketplaces"`
	Limit       int    `json:"limit,omitempty"      jsonschema:"Maximum number of recommendations to return; defaults to 10 when unset or 0"`
}

// PluginRecommendation is one ranked result in the recommend output.
type PluginRecommendation struct {
	AvailablePlugin
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale"`
}

// RecommendPluginsOutput is the typed output for the recommend_plugins tool.
type RecommendPluginsOutput struct {
	Recommendations []PluginRecommendation    `json:"recommendations"`
	Sources         []MarketplaceSourceStatus `json:"sources"`
	Context         string                    `json:"context"`
	Total           int                       `json:"total"`
	Truncated       bool                      `json:"truncated"`
}

// NewRecommendPlugins returns the recommend_plugins tool declaration.
func NewRecommendPlugins() Tool {
	const desc = "Given free-text context describing a task or need, rank uninstalled plugins from configured marketplace sources by relevance. Returns an ordered list with name, description, and relevance rationale."
	return Tool{
		Name:        "recommend_plugins",
		Description: desc,
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "recommend_plugins",
				Description: desc,
			}, HandleRecommendPlugins)
		},
	}
}

// HandleRecommendPlugins is the generic typed handler for recommend_plugins.
func HandleRecommendPlugins(ctx context.Context, _ *mcp.CallToolRequest, input RecommendPluginsInput) (*mcp.CallToolResult, RecommendPluginsOutput, error) {
	if strings.TrimSpace(input.Context) == "" {
		return errResult(skerrors.ErrInvalidInput, "context is required", ""), RecommendPluginsOutput{}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return errResult(skerrors.ErrMarketplaceUnreachable, "failed to load config", err.Error()), RecommendPluginsOutput{}, nil
	}

	queryTokens := tokenize(input.Context)
	if len(queryTokens) == 0 {
		return errResult(skerrors.ErrInvalidInput, "context contains no meaningful terms", ""), RecommendPluginsOutput{}, nil
	}

	out := RecommendPluginsOutput{
		Recommendations: []PluginRecommendation{},
		Sources:         []MarketplaceSourceStatus{},
		Context:         input.Context,
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
			score, rationale := scorePlugin(p, queryTokens)
			if score == 0 {
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
			out.Recommendations = append(out.Recommendations, PluginRecommendation{
				AvailablePlugin: ap,
				Score:           score,
				Rationale:       rationale,
			})
		}
	}

	// Sort by score descending, then name ascending for stability.
	sort.Slice(out.Recommendations, func(i, j int) bool {
		if out.Recommendations[i].Score != out.Recommendations[j].Score {
			return out.Recommendations[i].Score > out.Recommendations[j].Score
		}
		return out.Recommendations[i].Name < out.Recommendations[j].Name
	})

	const defaultLimit = 10
	limit := input.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	out.Total = len(out.Recommendations)
	out.Truncated = out.Total > limit
	if out.Truncated {
		out.Recommendations = out.Recommendations[:limit]
	}

	return nil, out, nil
}

// scorePlugin computes a relevance score in [0, 1] for p against queryTokens.
// Score = (matched distinct tokens) / (total query tokens), with a small bonus
// for name matches. Returns the score and a human-readable rationale string.
func scorePlugin(p marketplace.PluginEntry, queryTokens map[string]bool) (float64, string) {
	nameTokens := tokenize(p.Name)
	descTokens := tokenize(p.Description)
	catTokens := tokenize(p.Category)
	kwTokens := make(map[string]bool)
	for _, kw := range p.Keywords {
		for t := range tokenize(kw) {
			kwTokens[t] = true
		}
	}

	var matchedIn []string
	matched := make(map[string]bool)

	for qt := range queryTokens {
		if nameTokens[qt] {
			matched[qt] = true
			matchedIn = append(matchedIn, qt+" (name)")
		} else if descTokens[qt] {
			matched[qt] = true
			matchedIn = append(matchedIn, qt+" (description)")
		} else if kwTokens[qt] {
			matched[qt] = true
			matchedIn = append(matchedIn, qt+" (keyword)")
		} else if catTokens[qt] {
			matched[qt] = true
			matchedIn = append(matchedIn, qt+" (category)")
		}
	}

	if len(matched) == 0 {
		return 0, ""
	}

	sort.Strings(matchedIn)

	base := float64(len(matched)) / float64(len(queryTokens))

	// Small bonus (up to 0.1) when the plugin name itself contains query tokens.
	nameBonus := 0.0
	for qt := range queryTokens {
		if nameTokens[qt] {
			nameBonus += 0.1 / float64(len(queryTokens))
		}
	}
	score := base + nameBonus
	if score > 1.0 {
		score = 1.0
	}

	rationale := fmt.Sprintf("Matched %d/%d term(s): %s",
		len(matched), len(queryTokens), strings.Join(matchedIn, ", "))

	return score, rationale
}

// tokenize converts text into a set of lowercase alphabetic tokens, filtering
// out single-character tokens and common English stop words.
func tokenize(text string) map[string]bool {
	tokens := make(map[string]bool)
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		if len(w) <= 1 || stopWords[w] {
			continue
		}
		tokens[w] = true
	}
	return tokens
}

// stopWords is a small set of common English words that add no signal.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "up": true, "about": true,
	"into": true, "through": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true,
	"has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "can": true, "that": true, "this": true, "it": true,
	"its": true, "not": true, "as": true, "if": true, "so": true,
}
