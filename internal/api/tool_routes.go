package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/models"
)

func registerConnectorAPIRoutes(r chi.Router, d Deps) {
	r.Get("/connectors/{id}/apis", handleListAPIs(d))
	r.Post("/connectors/{id}/apis", handleCreateAPI(d))
	r.Patch("/connectors/{id}/apis/{apiId}", handleUpdateAPI(d))
	r.Put("/connectors/{id}/apis/{apiId}/credentials", handlePutAPICredentials(d))
}

func registerToolRoutes(r chi.Router, d Deps) {
	r.Get("/connectors/{id}/tools", handleListTools(d))
	r.Post("/connectors/{id}/tools", handleCreateTool(d))
	r.Get("/connectors/{id}/tools/{toolId}", handleGetTool(d))
	r.Patch("/connectors/{id}/tools/{toolId}", handleUpdateTool(d))
	r.Delete("/connectors/{id}/tools/{toolId}", handleDeleteTool(d))
	r.Post("/connectors/{id}/tools/{toolId}/run", handleRunTool(d))
}

func handleListAPIs(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		apis, err := d.APIs.ListByConnector(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, apis)
	}
}

func handleCreateAPI(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			Name       string            `json:"name"`
			EngineType models.EngineType `json:"engineType"`
			BaseURL    string            `json:"baseUrl"`
			AuthType   models.AuthType   `json:"authType"`
			SpecURL    string            `json:"specUrl"`
			GroupID    *int64            `json:"groupId"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		api, err := d.APIs.Create(r.Context(), orgIDFromContext(r), id, controlplane.CreateAPIInput{
			Name: body.Name, EngineType: body.EngineType, BaseURL: body.BaseURL,
			AuthType: body.AuthType, SpecURL: body.SpecURL, GroupID: body.GroupID,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, api)
	}
}

func handleUpdateAPI(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiID, ok := intIDParam(w, r, "apiId")
		if !ok {
			return
		}
		var body struct {
			Name     *string          `json:"name"`
			BaseURL  *string          `json:"baseUrl"`
			AuthType *models.AuthType `json:"authType"`
			IsActive *bool            `json:"isActive"`
			GroupID  nullableID       `json:"groupId"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		api, err := d.APIs.Update(r.Context(), orgIDFromContext(r), apiID, controlplane.UpdateAPIInput{
			Name: body.Name, BaseURL: body.BaseURL, AuthType: body.AuthType,
			IsActive: body.IsActive, GroupID: body.GroupID.ptr(),
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, api)
	}
}

func handlePutAPICredentials(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiID, ok := intIDParam(w, r, "apiId")
		if !ok {
			return
		}
		var body controlplane.PutCredentialsInput
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if err := d.APIs.PutCredentials(r.Context(), orgIDFromContext(r), apiID, body); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func resolveAPIID(w http.ResponseWriter, r *http.Request, d Deps, connectorID int64) (int64, bool) {
	explicit, ok := intQueryParam(w, r, "apiId")
	if !ok {
		return 0, false
	}
	if explicit != 0 {
		return explicit, true
	}

	apis, err := d.APIs.ListByConnector(r.Context(), orgIDFromContext(r), connectorID)
	if err != nil {
		handleServiceError(w, r, d.Logger, err)
		return 0, false
	}
	switch len(apis) {
	case 1:
		return apis[0].ID, true
	case 0:
		apiError(w, r, http.StatusNotFound, "not_found", "connector has no APIs")
		return 0, false
	default:
		apiError(w, r, http.StatusBadRequest, "ambiguous_api",
			"connector has multiple APIs; pass ?apiId= to disambiguate")
		return 0, false
	}
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
		tools, err := d.Tools.ListByConnectorAPI(r.Context(), orgIDFromContext(r), apiID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
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
		orgID := orgIDFromContext(r)

		api, err := d.APIs.Get(r.Context(), orgID, apiID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		var body controlplane.CreateToolInput
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if body.GroupID == nil {

			body.GroupID = api.GroupID
		}

		tool, err := d.Tools.Create(r.Context(), orgID, apiID, api.EngineType, body)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
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
		tool, err := d.Tools.Get(r.Context(), orgIDFromContext(r), toolID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, tool)
	}
}

func handleUpdateTool(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		toolID, ok := intIDParam(w, r, "toolId")
		if !ok {
			return
		}
		var body struct {
			Name            *string            `json:"name"`
			Method          *models.HTTPMethod `json:"method"`
			Path            *string            `json:"path"`
			Description     *string            `json:"description"`
			Parameters      map[string]any     `json:"parameters"`
			EndpointMapping map[string]any     `json:"endpointMapping"`
			ResponseMapping map[string]any     `json:"responseMapping"`
			OutputSchema    map[string]any     `json:"outputSchema"`
			Cached          *bool              `json:"cached"`
			CacheTTLSeconds *int               `json:"cacheTtlSeconds"`
			GroupID         nullableID         `json:"groupId"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		tool, err := d.Tools.Update(r.Context(), orgIDFromContext(r), toolID, controlplane.UpdateToolInput{
			Name: body.Name, Method: body.Method, Path: body.Path, Description: body.Description,
			Parameters: body.Parameters, EndpointMapping: body.EndpointMapping,
			ResponseMapping: body.ResponseMapping, OutputSchema: body.OutputSchema,
			Cached: body.Cached, CacheTTLSeconds: body.CacheTTLSeconds, GroupID: body.GroupID.ptr(),
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
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
		if err := d.Tools.Delete(r.Context(), orgIDFromContext(r), toolID); err != nil {
			handleServiceError(w, r, d.Logger, err)
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
		orgID := orgIDFromContext(r)

		tool, err := d.Tools.Get(r.Context(), orgID, toolID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		api, err := d.APIs.Get(r.Context(), orgID, tool.ConnectorAPIID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		var input map[string]any
		if r.ContentLength != 0 {
			if err := decodeJSON(w, r, d.MaxRequestBytes, &input); err != nil {
				apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
		}

		user, ok := identity.UserFromContext(r.Context())
		if !ok {
			apiError(w, r, http.StatusUnauthorized, "unauthorized", "no active session")
			return
		}

		outcome, err := d.Dispatcher.InvokeDirect(r.Context(), tool, api, input, dispatcher.Identity{
			OrganizationID: orgID, UserID: user.ID, UserEmail: user.Email,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}
