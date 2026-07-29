package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/slug"
)

func registerConnectorRoutes(r chi.Router, d Deps) {
	r.Get("/connectors", handleListConnectors(d))
	r.Post("/connectors", handleCreateConnector(d))
	r.Post("/connectors/import", handleImportConnector(d))
	r.Get("/connectors/{id}", handleGetConnector(d))
	r.Patch("/connectors/{id}", handleUpdateConnector(d))
	r.Delete("/connectors/{id}", handleDeleteConnector(d))
	r.Post("/connectors/{id}/toggle", handleToggleConnector(d))
	r.Post("/connectors/{id}/health-check", handleHealthCheckConnector(d))
	r.Get("/connectors/{id}/export", handleExportConnector(d))

	r.Get("/connectors/{id}/modules", handleListConnectorModules(d))
	r.Post("/connectors/{id}/modules", handleCreateConnectorModule(d))

	registerConnectorAPIRoutes(r, d)
	registerToolRoutes(r, d)
}

func handleListConnectors(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := pagination(r)
		list, total, err := d.Connectors.List(r.Context(), orgIDFromContext(r), controlplane.ListConnectorsFilter{
			Type:     r.URL.Query().Get("type"),
			Status:   r.URL.Query().Get("status"),
			Query:    r.URL.Query().Get("q"),
			Page:     p.Page,
			PageSize: p.PageSize,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, paginated(list, p, total))
	}
}

type moduleAPIBody struct {
	Name        string                            `json:"name"`
	EngineType  models.EngineType                 `json:"engineType"`
	BaseURL     string                            `json:"baseUrl"`
	AuthType    models.AuthType                   `json:"authType"`
	SpecURL     string                            `json:"specUrl"`
	Credentials *controlplane.PutCredentialsInput `json:"credentials"`
}

type moduleBody struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	APIs        []moduleAPIBody `json:"apis"`
}

type moduleResult struct {
	Module *models.ToolGroup      `json:"module"`
	APIs   []*models.ConnectorAPI `json:"apis"`
}

func createConnectorModule(ctx context.Context, d Deps, orgID, connectorID int64, in moduleBody) (*moduleResult, error) {
	group, err := d.Groups.Create(ctx, orgID, controlplane.CreateGroupInput{
		Name: in.Name, Description: in.Description,
	})
	if err != nil {
		return nil, err
	}

	apis := make([]*models.ConnectorAPI, 0, len(in.APIs))
	for _, a := range in.APIs {
		api, err := d.APIs.Create(ctx, orgID, connectorID, controlplane.CreateAPIInput{
			Name: a.Name, EngineType: a.EngineType, BaseURL: a.BaseURL,
			AuthType: a.AuthType, SpecURL: a.SpecURL, GroupID: &group.ID,
		})
		if err != nil {
			return nil, err
		}
		if a.Credentials != nil {
			if err := d.APIs.PutCredentials(ctx, orgID, api.ID, *a.Credentials); err != nil {
				return nil, err
			}
		}
		apis = append(apis, api)
	}
	return &moduleResult{Module: group, APIs: apis}, nil
}

func handleCreateConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name        string            `json:"name"`
			Glyph       string            `json:"glyph"`
			Description string            `json:"description"`
			Type        models.EngineType `json:"type"`
			BaseURL     string            `json:"baseUrl"`
			AuthType    models.AuthType   `json:"authType"`
			Modules     []moduleBody      `json:"modules"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if body.Name == "" {
			apiError(w, r, http.StatusBadRequest, "validation_failed", "name is required")
			return
		}

		orgID := orgIDFromContext(r)
		connector, err := d.Connectors.Create(r.Context(), orgID, controlplane.CreateConnectorInput{
			Name: body.Name, Glyph: body.Glyph, Description: body.Description,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		modules := make([]*moduleResult, 0, len(body.Modules))
		switch {
		case len(body.Modules) > 0:
			for _, m := range body.Modules {
				result, err := createConnectorModule(r.Context(), d, orgID, connector.ID, m)
				if err != nil {
					handleServiceError(w, r, d.Logger, err)
					return
				}
				modules = append(modules, result)
			}
		case body.BaseURL != "":

			if _, err := d.APIs.Create(r.Context(), orgID, connector.ID, controlplane.CreateAPIInput{
				Name: body.Name, EngineType: body.Type, BaseURL: body.BaseURL, AuthType: body.AuthType,
			}); err != nil {
				handleServiceError(w, r, d.Logger, err)
				return
			}
		}

		connector, err = d.Connectors.Get(r.Context(), orgID, connector.ID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"connector": connector, "modules": modules})
	}
}

func handleListConnectorModules(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		orgID := orgIDFromContext(r)

		groupIDs, err := d.Groups.GroupsForConnector(r.Context(), orgID, id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		out := make([]*moduleResult, 0, len(groupIDs))
		for _, groupID := range groupIDs {
			group, err := d.Groups.Get(r.Context(), orgID, groupID)
			if err != nil {
				handleServiceError(w, r, d.Logger, err)
				return
			}
			apis, err := d.APIs.ListByGroup(r.Context(), orgID, id, groupID)
			if err != nil {
				handleServiceError(w, r, d.Logger, err)
				return
			}
			out = append(out, &moduleResult{Module: group, APIs: apis})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func handleCreateConnectorModule(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body moduleBody
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		result, err := createConnectorModule(r.Context(), d, orgIDFromContext(r), id, body)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func handleGetConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		c, err := d.Connectors.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func handleUpdateConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			Name        *string                 `json:"name"`
			Description *string                 `json:"description"`
			Status      *models.ConnectorStatus `json:"status"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		c, err := d.Connectors.Update(r.Context(), orgIDFromContext(r), id, controlplane.UpdateConnectorInput{
			Name: body.Name, Description: body.Description, Status: body.Status,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func handleToggleConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			Active bool `json:"active"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		c, err := d.Connectors.SetActive(r.Context(), orgIDFromContext(r), id, body.Active)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func handleDeleteConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Connectors.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleHealthCheckConnector(d Deps) http.HandlerFunc {
	ping := func(ctx context.Context, baseURL string) error {
		req, err := httpx.NewRequest(ctx, http.MethodGet, baseURL, nil)
		if err != nil {
			return err
		}
		resp, err := d.Upstream.Do(ctx, req)
		if err != nil {
			return err
		}

		if resp.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("upstream returned %d", resp.StatusCode)
		}
		return nil
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		c, err := d.Connectors.HealthCheck(r.Context(), orgIDFromContext(r), id, ping)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func handleImportConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "openapi"
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			name = "Imported connector"
		}

		parser, ok := d.Parsers.For(format)
		if !ok {
			apiError(w, r, http.StatusBadRequest, "unknown_format",
				fmt.Sprintf("no parser registered for format %q; known formats: %v", format, d.Parsers.Formats()))
			return
		}

		raw, err := readBody(w, r, d.MaxRequestBytes)
		if err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		result, err := parser.Parse(raw)
		if err != nil {

			apiError(w, r, http.StatusBadRequest, "parse_failed", err.Error())
			return
		}

		orgID := orgIDFromContext(r)
		connector, err := d.Connectors.Create(r.Context(), orgID, controlplane.CreateConnectorInput{Name: name})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		api, err := d.APIs.Create(r.Context(), orgID, connector.ID, controlplane.CreateAPIInput{
			Name: name, EngineType: result.EngineType, BaseURL: result.BaseURL, AuthType: models.AuthNone,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		tools, err := d.Tools.BulkCreate(r.Context(), orgID, api.ID, result.EngineType, result.Tools)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		connector, err = d.Connectors.Get(r.Context(), orgID, connector.ID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"connector": connector, "api": api, "tools": tools,
		})
	}
}

type apiExport struct {
	API   *models.ConnectorAPI `json:"api"`
	Tools []*models.MCPTool    `json:"tools"`
}

func handleExportConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		orgID := orgIDFromContext(r)

		connector, err := d.Connectors.Get(r.Context(), orgID, id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		apis, err := d.APIs.ListByConnector(r.Context(), orgID, id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		exports := make([]apiExport, 0, len(apis))
		for _, a := range apis {
			tools, err := d.Tools.ListByConnectorAPI(r.Context(), orgID, a.ID)
			if err != nil {
				handleServiceError(w, r, d.Logger, err)
				return
			}
			exports = append(exports, apiExport{API: a, Tools: tools})
		}

		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s.json"`, slug.Make(connector.Name)))
		writeJSON(w, http.StatusOK, map[string]any{"connector": connector, "apis": exports})
	}
}
