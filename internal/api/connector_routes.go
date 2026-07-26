package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/models"
)

func registerConnectorRoutes(r chi.Router, d Deps) {
	r.Get("/connectors", handleListConnectors(d))
	r.Post("/connectors", handleCreateConnector(d))
	r.Post("/connectors/import", handleImportConnector(d))
	r.Get("/connectors/{id}", handleGetConnector(d))
	r.Patch("/connectors/{id}", handleUpdateConnector(d))
	r.Delete("/connectors/{id}", handleDeleteConnector(d))
	r.Post("/connectors/{id}/health-check", handleHealthCheckConnector(d))
	r.Get("/connectors/{id}/export", handleExportConnector(d))

	r.Get("/connectors/{id}/apis", handleListAPIs(d))
	r.Post("/connectors/{id}/apis", handleCreateAPI(d))
	r.Put("/connectors/{id}/apis/{apiId}/credentials", handlePutAPICredentials(d))

	r.Get("/connectors/{id}/tools", handleListTools(d))
	r.Post("/connectors/{id}/tools", handleCreateTool(d))
	r.Get("/connectors/{id}/tools/{toolId}", handleGetTool(d))
	r.Patch("/connectors/{id}/tools/{toolId}", handleUpdateTool(d))
	r.Delete("/connectors/{id}/tools/{toolId}", handleDeleteTool(d))
	r.Post("/connectors/{id}/tools/{toolId}/run", handleRunTool(d))
}

func handleListConnectors(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := pagination(r)
		list, total, err := d.Connectors.List(r.Context(), orgIDFromContext(r), controlplane.ListConnectorsFilter{
			Type: r.URL.Query().Get("type"), Status: r.URL.Query().Get("status"), Query: r.URL.Query().Get("q"),
			Page: p.Page, PageSize: p.PageSize,
		})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, paginated(list, p, total))
	}
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
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		c, err := d.Connectors.Create(r.Context(), orgIDFromContext(r), controlplane.CreateConnectorInput{
			Name: body.Name, Glyph: body.Glyph, Description: body.Description,
		})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		if body.BaseURL != "" {
			if _, err := d.APIs.Create(r.Context(), c.ID, controlplane.CreateAPIInput{
				Name: body.Name, EngineType: body.Type, BaseURL: body.BaseURL, AuthType: body.AuthType,
			}); err != nil {
				handleServiceError(w, err)
				return
			}
		}
		c, err = d.Connectors.Get(r.Context(), orgIDFromContext(r), c.ID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, c)
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
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func handleUpdateConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name        *string                 `json:"name"`
			Description *string                 `json:"description"`
			Status      *models.ConnectorStatus `json:"status"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		c, err := d.Connectors.Update(r.Context(), orgIDFromContext(r), id, controlplane.UpdateConnectorInput{
			Name: body.Name, Description: body.Description, Status: body.Status,
		})
		if err != nil {
			handleServiceError(w, err)
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
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleHealthCheckConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := &http.Client{Timeout: 5 * time.Second}
		ping := func(baseURL string) error {
			resp, err := client.Get(baseURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("upstream returned %d", resp.StatusCode)
			}
			return nil
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		c, err := d.Connectors.HealthCheck(r.Context(), orgIDFromContext(r), id, ping)
		if err != nil {
			handleServiceError(w, err)
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
			apiError(w, http.StatusBadRequest, "unknown_format", fmt.Sprintf("no parser registered for format %q", format))
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		result, err := parser.Parse(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "parse_failed", err.Error())
			return
		}

		orgID := orgIDFromContext(r)
		connector, err := d.Connectors.Create(r.Context(), orgID, controlplane.CreateConnectorInput{Name: name})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		api, err := d.APIs.Create(r.Context(), connector.ID, controlplane.CreateAPIInput{
			Name: name, EngineType: result.EngineType, BaseURL: result.BaseURL, AuthType: models.AuthNone,
		})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		tools, err := d.Tools.BulkCreate(r.Context(), api.ID, result.EngineType, result.Tools)
		if err != nil {
			handleServiceError(w, err)
			return
		}

		connector, err = d.Connectors.Get(r.Context(), orgID, connector.ID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"connector": connector, "api": api, "tools": tools})
	}
}

func handleExportConnector(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := orgIDFromContext(r)
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		connector, err := d.Connectors.Get(r.Context(), orgID, id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		apis, err := d.APIs.ListByConnector(r.Context(), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		export := map[string]any{"connector": connector, "apis": []map[string]any{}}
		var apiExports []map[string]any
		for _, a := range apis {
			tools, err := d.Tools.ListByConnectorAPI(r.Context(), a.ID)
			if err != nil {
				handleServiceError(w, err)
				return
			}
			apiExports = append(apiExports, map[string]any{"api": a, "tools": tools})
		}
		export["apis"] = apiExports

		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, connector.Name))
		writeJSON(w, http.StatusOK, export)
	}
}

func handleListAPIs(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		apis, err := d.APIs.ListByConnector(r.Context(), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, apis)
	}
}

func handleCreateAPI(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name       string            `json:"name"`
			EngineType models.EngineType `json:"engineType"`
			BaseURL    string            `json:"baseUrl"`
			AuthType   models.AuthType   `json:"authType"`
			SpecURL    string            `json:"specUrl"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		api, err := d.APIs.Create(r.Context(), id, controlplane.CreateAPIInput{
			Name: body.Name, EngineType: body.EngineType, BaseURL: body.BaseURL, AuthType: body.AuthType, SpecURL: body.SpecURL,
		})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, api)
	}
}

func handlePutAPICredentials(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.PutCredentialsInput
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		apiID, ok := intIDParam(w, r, "apiId")
		if !ok {
			return
		}
		if err := d.APIs.PutCredentials(r.Context(), apiID, body); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func resolveAPIID(w http.ResponseWriter, r *http.Request, d Deps, connectorID int64) (int64, bool) {
	if explicit, ok := intQueryParam(w, r, "apiId"); !ok {
		return 0, false
	} else if explicit != 0 {
		return explicit, true
	}
	apis, err := d.APIs.ListByConnector(r.Context(), connectorID)
	if err != nil {
		handleServiceError(w, err)
		return 0, false
	}
	if len(apis) == 1 {
		return apis[0].ID, true
	}
	apiError(w, http.StatusBadRequest, "ambiguous_api", "connector has multiple APIs; pass ?apiId= to disambiguate")
	return 0, false
}

func handleListTools(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connectorID, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		apiID, ok := resolveAPIID(w, r, d, connectorID)
		if !ok {
			return
		}
		tools, err := d.Tools.ListByConnectorAPI(r.Context(), apiID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tools)
	}
}

func handleCreateTool(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connectorID, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		apiID, ok := resolveAPIID(w, r, d, connectorID)
		if !ok {
			return
		}
		api, err := d.APIs.Get(r.Context(), apiID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		var body controlplane.CreateToolInput
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		tool, err := d.Tools.Create(r.Context(), apiID, api.EngineType, body)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, tool)
	}
}

func handleGetTool(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		toolID, ok := intIDParam(w, r, "toolId")
		if !ok {
			return
		}
		tool, err := d.Tools.Get(r.Context(), toolID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tool)
	}
}

func handleUpdateTool(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.UpdateToolInput
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		toolID, ok := intIDParam(w, r, "toolId")
		if !ok {
			return
		}
		tool, err := d.Tools.Update(r.Context(), toolID, body)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tool)
	}
}

func handleDeleteTool(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		toolID, ok := intIDParam(w, r, "toolId")
		if !ok {
			return
		}
		if err := d.Tools.Delete(r.Context(), toolID); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRunTool(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		toolID, ok := intIDParam(w, r, "toolId")
		if !ok {
			return
		}
		tool, err := d.Tools.Get(r.Context(), toolID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		api, err := d.APIs.Get(r.Context(), tool.ConnectorAPIID)
		if err != nil {
			handleServiceError(w, err)
			return
		}

		var input map[string]any
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
		}

		user, _ := identity.UserFromContext(r.Context())
		outcome, err := d.Dispatcher.InvokeDirect(r.Context(), tool, api, input, dispatcher.Identity{
			OrganizationID: orgIDFromContext(r), UserID: user.ID, UserEmail: user.Email,
		})
		if err != nil {
			apiError(w, http.StatusBadGateway, "dispatch_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}
