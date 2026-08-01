package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/parsers"
)

func registerMarketplaceRoutes(r chi.Router, d Deps) {
	r.Get("/marketplace", handleListMarketplace(d))
	r.Get("/marketplace/{key}", handleGetMarketplaceTemplate(d))
	r.Get("/marketplace/{key}/tools", handleListMarketplaceTemplateTools(d))
	r.Post("/marketplace/{key}/install", handleInstallMarketplaceTemplate(d))
}

// loadTemplateSpec fetches the template by key and parses its bundled spec.
// On failure it writes the appropriate error response itself and returns ok=false.
func loadTemplateSpec(w http.ResponseWriter, r *http.Request, d Deps, key string) (*models.AdapterTemplate, *parsers.ParseResult, bool) {
	template, err := d.Marketplace.Get(r.Context(), key)
	if err != nil {
		handleServiceError(w, r, d.Logger, err)
		return nil, nil, false
	}

	parser, ok := d.Parsers.For(template.SpecFormat)
	if !ok {
		apiError(w, r, http.StatusInternalServerError, "unknown_format",
			"no parser registered for this template's spec format")
		return nil, nil, false
	}
	result, err := parser.Parse(template.SpecRaw)
	if err != nil {
		apiError(w, r, http.StatusInternalServerError, "parse_failed", err.Error())
		return nil, nil, false
	}
	return template, result, true
}

type marketplaceToolPreview struct {
	Name        string            `json:"name"`
	Method      models.HTTPMethod `json:"method"`
	Path        string            `json:"path"`
	Description string            `json:"description"`
	Parameters  map[string]any    `json:"parameters"`
}

func handleListMarketplaceTemplateTools(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		_, result, ok := loadTemplateSpec(w, r, d, key)
		if !ok {
			return
		}

		tools := make([]marketplaceToolPreview, 0, len(result.Tools))
		for _, t := range result.Tools {
			tools = append(tools, marketplaceToolPreview{
				Name: t.Name, Method: t.Method, Path: t.Path,
				Description: t.Description, Parameters: t.Parameters,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
	}
}

func handleListMarketplace(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templates, err := d.Marketplace.List(r.Context(), r.URL.Query().Get("category"), r.URL.Query().Get("q"))
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
	}
}

func handleGetMarketplaceTemplate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		t, err := d.Marketplace.Get(r.Context(), key)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, t)
	}
}

func handleInstallMarketplaceTemplate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")

		var body struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		template, result, ok := loadTemplateSpec(w, r, d, key)
		if !ok {
			return
		}

		name := body.Name
		if name == "" {
			name = template.Name
		}

		orgID := orgIDFromContext(r)

		// Every install gets its own dedicated tool group so the installed
		// tools are usable (attachable to an MCP server) without a separate
		// manual grouping step.
		group, err := d.Groups.Create(r.Context(), orgID, controlplane.CreateGroupInput{
			Name: name, Description: template.Description,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		connector, err := d.Connectors.Create(r.Context(), orgID, controlplane.CreateConnectorInput{
			Name: name, Glyph: template.Glyph, Description: template.Description,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		api, err := d.APIs.Create(r.Context(), orgID, connector.ID, controlplane.CreateAPIInput{
			Name: template.Name, EngineType: result.EngineType, BaseURL: template.BaseURL, AuthType: template.AuthType,
			GroupID: &group.ID,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		tools, err := d.Tools.BulkCreate(r.Context(), orgID, api.ID, result.EngineType, result.Tools, &group.ID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		if err := d.Marketplace.IncrementInstallCount(r.Context(), template.ID); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		connector, err = d.Connectors.Get(r.Context(), orgID, connector.ID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"connector": connector, "api": api, "tools": tools, "module": group,
		})
	}
}
