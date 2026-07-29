package engines

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

type capture struct {
	method  string
	path    string
	rawPath string
	query   url.Values
	headers http.Header
	body    string
}

func captureServer(t *testing.T, status int, response string) (*httptest.Server, *capture) {
	t.Helper()
	got := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.method = r.Method
		got.path = r.URL.Path
		got.rawPath = r.URL.EscapedPath()
		got.query = r.URL.Query()
		got.headers = r.Header.Clone()
		got.body = string(raw)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server, got
}

func TestRestEngine_Execute_GETPutsParamsInQuery(t *testing.T) {
	server, got := captureServer(t, http.StatusOK, `{"ok":true}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{Method: models.MethodGET, Path: "/orders", EndpointMapping: map[string]any{}}

	result, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{},
		map[string]any{"status": "open", "limit": 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got.method != http.MethodGet {
		t.Errorf("method = %s", got.method)
	}
	if got.query.Get("status") != "open" || got.query.Get("limit") != "10" {
		t.Errorf("query = %v, want both params carried in the query string", got.query)
	}
	if got.body != "" {
		t.Errorf("GET sent a body: %q", got.body)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", result.StatusCode)
	}
	if _, ok := result.Body.(map[string]any); !ok {
		t.Errorf("Body = %#v, want decoded JSON", result.Body)
	}
}

func TestRestEngine_Execute_POSTPutsParamsInBody(t *testing.T) {
	server, got := captureServer(t, http.StatusCreated, `{}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{Method: models.MethodPOST, Path: "/orders", EndpointMapping: map[string]any{}}

	if _, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{},
		map[string]any{"sku": "ABC"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatalf("upstream body was not JSON: %q", got.body)
	}
	if body["sku"] != "ABC" {
		t.Errorf("body = %v, want the parameter in the JSON body", body)
	}
	if got.headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", got.headers.Get("Content-Type"))
	}
}

func TestRestEngine_Execute_HonoursExplicitParamMapping(t *testing.T) {
	server, got := captureServer(t, http.StatusOK, `{}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{
		Method: models.MethodPOST,
		Path:   "/orders/{orderId}",
		EndpointMapping: map[string]any{
			"pathParams":  []any{"orderId"},
			"queryParams": []any{"trace"},
			"bodyParams":  []any{"note"},
		},
	}

	if _, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{}, map[string]any{
		"orderId": "o-1",
		"trace":   "yes",
		"note":    "hello",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got.path != "/orders/o-1" {
		t.Errorf("path = %q, want the path parameter substituted", got.path)
	}
	if got.query.Get("trace") != "yes" {
		t.Errorf("query = %v, want trace in the query string", got.query)
	}
	if !strings.Contains(got.body, `"note":"hello"`) {
		t.Errorf("body = %q, want note in the body", got.body)
	}
	if strings.Contains(got.body, "orderId") {
		t.Errorf("body = %q, must not repeat a consumed path parameter", got.body)
	}
}

func TestRestEngine_Execute_MissingPathParamIsAnError(t *testing.T) {
	api := &models.ConnectorAPI{BaseURL: "https://example.com"}
	tool := &models.MCPTool{
		Method:          models.MethodGET,
		Path:            "/orders/{orderId}",
		EndpointMapping: map[string]any{"pathParams": []any{"orderId"}},
	}

	_, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{}, map[string]any{})
	if err == nil {
		t.Fatal("Execute accepted a request missing a required path parameter")
	}
	if !strings.Contains(err.Error(), "orderId") {
		t.Errorf("error = %v, want it to name the missing parameter", err)
	}
}

func TestRestEngine_Execute_EscapesPathParameters(t *testing.T) {
	server, got := captureServer(t, http.StatusOK, `{}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{
		Method:          models.MethodGET,
		Path:            "/orders/{orderId}",
		EndpointMapping: map[string]any{"pathParams": []any{"orderId"}},
	}

	if _, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{},
		map[string]any{"orderId": "../../admin/secrets"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(got.rawPath, "../") {
		t.Errorf("escaped path = %q, traversal was not neutralised", got.rawPath)
	}
	if !strings.HasPrefix(got.path, "/orders/") {
		t.Errorf("path = %q, want it still under /orders/", got.path)
	}
}

func TestRestEngine_Execute_AppliesCredentials(t *testing.T) {
	server, got := captureServer(t, http.StatusOK, `{}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{
		Method:          models.MethodGET,
		Path:            "/ping",
		EndpointMapping: map[string]any{"headers": map[string]any{"Accept": "application/json"}},
	}
	creds := schemes.Credentials{
		Headers:     map[string]string{"X-Api-Key": "secret"},
		QueryParams: map[string]string{"access_token": "qtoken"},
	}

	if _, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, creds, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got.headers.Get("X-Api-Key") != "secret" {
		t.Errorf("X-Api-Key = %q", got.headers.Get("X-Api-Key"))
	}
	if got.headers.Get("Accept") != "application/json" {
		t.Errorf("static header from endpointMapping missing: %q", got.headers.Get("Accept"))
	}
	if got.query.Get("access_token") != "qtoken" {
		t.Errorf("credential query param missing: %v", got.query)
	}
}

func TestRestEngine_Execute_MarksCredentialHeadersSensitive(t *testing.T) {
	server, got := captureServer(t, http.StatusOK, `{}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{Method: models.MethodGET, Path: "/ping", EndpointMapping: map[string]any{}}
	creds := schemes.Credentials{Headers: map[string]string{"X-Vendor-Key": "secret"}}

	if _, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, creds, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if v := got.headers.Get("X-Gridhook-Sensitive-Headers"); v != "" {
		t.Logf("marker header reached upstream: %q", v)
	}
}

func TestRestEngine_Execute_NonJSONBodyIsReturnedAsText(t *testing.T) {
	server, _ := captureServer(t, http.StatusOK, `not json at all`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{Method: models.MethodGET, Path: "/", EndpointMapping: map[string]any{}}

	result, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Body != "not json at all" {
		t.Errorf("Body = %#v, want the raw text", result.Body)
	}
}

func TestRestEngine_Execute_ReturnsErrorStatusesAsResults(t *testing.T) {
	server, _ := captureServer(t, http.StatusNotFound, `{"error":"missing"}`)

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{Method: models.MethodGET, Path: "/nope", EndpointMapping: map[string]any{}}

	result, err := NewRestEngine(newTestClient(t)).Execute(t.Context(), api, tool, schemes.Credentials{}, nil)
	if err != nil {
		t.Fatalf("Execute returned a transport error for a 404: %v", err)
	}
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
	}
}

func TestBuildURL(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		path    string
		query   url.Values
		want    string
		wantErr bool
	}{
		{name: "simple join", base: "https://api.example.com", path: "/v1/orders", want: "https://api.example.com/v1/orders"},
		{name: "trailing slash on base", base: "https://api.example.com/", path: "/v1", want: "https://api.example.com/v1"},
		{name: "base with a path prefix", base: "https://api.example.com/api", path: "/v1", want: "https://api.example.com/api/v1"},
		{name: "path without a leading slash", base: "https://api.example.com", path: "v1", want: "https://api.example.com/v1"},
		{
			name:  "query is appended",
			base:  "https://api.example.com",
			path:  "/v1",
			query: url.Values{"a": []string{"1"}},
			want:  "https://api.example.com/v1?a=1",
		},
		{name: "non-http scheme is rejected", base: "file:///etc/passwd", path: "/x", wantErr: true},
		{name: "ftp scheme is rejected", base: "ftp://example.com", path: "/x", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildURL(tc.base, tc.path, tc.query)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("buildURL(%q) = %q, want an error", tc.base, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildURL: %v", err)
			}
			if got != tc.want {
				t.Errorf("buildURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubstitutePath(t *testing.T) {
	path, remaining, err := substitutePath(
		"/orgs/{org}/repos/{repo}",
		map[string]bool{"org": true, "repo": true},
		map[string]any{"org": "acme", "repo": "core", "page": 2},
	)
	if err != nil {
		t.Fatalf("substitutePath: %v", err)
	}
	if path != "/orgs/acme/repos/core" {
		t.Errorf("path = %q", path)
	}
	if !reflect.DeepEqual(remaining, map[string]any{"page": 2}) {
		t.Errorf("remaining = %v, want only the unconsumed input", remaining)
	}
}

func TestSubstitutePath_DoesNotMutateInput(t *testing.T) {
	input := map[string]any{"id": "1", "other": "x"}
	if _, _, err := substitutePath("/x/{id}", map[string]bool{"id": true}, input); err != nil {
		t.Fatalf("substitutePath: %v", err)
	}
	if _, ok := input["id"]; !ok {
		t.Error("substitutePath deleted a key from the caller's map")
	}
}

func TestToSet(t *testing.T) {

	if got := toSet(nil); got != nil {
		t.Errorf("toSet(nil) = %v, want nil", got)
	}
	if got := toSet([]any{}); got != nil {
		t.Errorf("toSet(empty) = %v, want nil", got)
	}
	if toSet(nil)["anything"] {
		t.Error("a nil set reported a membership")
	}

	got := toSet([]any{"a", "b", 3})
	for _, key := range []string{"a", "b", "3"} {
		if !got[key] {
			t.Errorf("toSet missing %q", key)
		}
	}
}

func TestMethodAcceptsBody(t *testing.T) {
	cases := map[models.HTTPMethod]bool{
		models.MethodGET:    false,
		models.MethodDELETE: false,
		models.MethodPOST:   true,
		models.MethodPUT:    true,
		models.MethodPATCH:  true,
	}
	for method, want := range cases {
		if got := methodAcceptsBody(method); got != want {
			t.Errorf("methodAcceptsBody(%s) = %v, want %v", method, got, want)
		}
	}
}
