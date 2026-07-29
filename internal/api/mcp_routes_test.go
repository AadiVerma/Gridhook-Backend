package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func mountSlugCapture(mount func(chi.Router, func(http.Handler) http.Handler)) string {
	var seen string
	capture := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = chi.URLParam(r, "slug")
			next.ServeHTTP(w, r)
		})
	}

	router := chi.NewRouter()
	mount(router, capture)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/acme-prod/tools", nil))
	return seen
}

func TestChiRouting_SlugIsInvisibleInMountLevelMiddleware(t *testing.T) {
	seen := mountSlugCapture(func(router chi.Router, capture func(http.Handler) http.Handler) {
		router.Route("/mcp", func(r chi.Router) {
			r.Use(capture)
			r.Get("/{slug}/tools", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	if seen != "" {
		t.Fatalf("slug = %q from mount-level middleware; this test documents why the "+
			"old routing shape was broken and should be updated if chi's behaviour changes", seen)
	}
}

func TestChiRouting_SlugIsVisibleWhenPartOfTheParentPattern(t *testing.T) {
	seen := mountSlugCapture(func(router chi.Router, capture func(http.Handler) http.Handler) {
		router.Route("/mcp", func(r chi.Router) {
			r.Route("/{slug}", func(r chi.Router) {
				r.Use(capture)
				r.Get("/tools", func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			})
		})
	})

	if seen != "acme-prod" {
		t.Errorf("slug = %q, want %q — the API key / server binding check depends on this",
			seen, "acme-prod")
	}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"absent", "", ""},
		{"bearer", "Bearer abc123", "abc123"},
		{"case-insensitive scheme", "bearer abc123", "abc123"},
		{"surrounding space trimmed", "Bearer   abc123  ", "abc123"},
		{"wrong scheme", "Basic abc123", ""},
		{"scheme only", "Bearer", ""},
		{"scheme and space only", "Bearer ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := bearerToken(r); got != tc.want {
				t.Errorf("bearerToken(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}
