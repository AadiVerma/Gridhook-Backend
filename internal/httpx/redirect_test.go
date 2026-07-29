package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Do_StripsCredentialsOnCrossHostRedirect(t *testing.T) {
	received := make(chan http.Header, 1)
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/landing", http.StatusFound)
	}))
	defer origin.Close()

	req, err := NewRequest(t.Context(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer super-secret")
	req.Header.Set("X-Api-Key", "live_key_do_not_leak")
	req.Header.Set("X-Vendor-Token", "vendor_secret")
	req.Header.Set("Accept", "application/json")

	MarkSensitive(req.Header, "X-Vendor-Token")

	if _, err := newClient(t, testConfig()).Do(t.Context(), req); err != nil {
		t.Fatalf("Do: %v", err)
	}

	got := <-received
	for _, header := range []string{"Authorization", "X-Api-Key", "X-Vendor-Token"} {
		if v := got.Get(header); v != "" {
			t.Errorf("%s leaked to the redirect target: %q", header, v)
		}
	}

	if v := got.Get(HeaderSensitiveNames); v != "" {
		t.Errorf("internal marker header leaked: %q", v)
	}

	if got.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q, want it preserved across the redirect", got.Get("Accept"))
	}
}

func TestClient_Do_KeepsCredentialsOnSameHostRedirect(t *testing.T) {
	received := make(chan http.Header, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			received <- r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, server.URL+"/final", http.StatusFound)
	}))
	defer server.Close()

	req, err := NewRequest(t.Context(), http.MethodGet, server.URL+"/start", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Api-Key", "live_key")
	MarkSensitive(req.Header, "X-Api-Key")

	if _, err := newClient(t, testConfig()).Do(t.Context(), req); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := (<-received).Get("X-Api-Key"); got != "live_key" {
		t.Errorf("X-Api-Key = %q, want it preserved on a same-host redirect", got)
	}
}

func TestClient_Do_StopsAfterMaxRedirects(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/again", http.StatusFound)
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.MaxRedirects = 3

	req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := newClient(t, cfg).Do(t.Context(), req); err == nil {
		t.Error("Do returned nil for an infinite redirect loop, want an error")
	}
}

func TestStripCredentialHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer x")
	h.Set("Cookie", "session=x")
	h.Set("X-Api-Key", "x")
	h.Set("Apikey", "x")
	h.Set("X-Amz-Security-Token", "x")
	h.Set("Content-Type", "application/json")
	MarkSensitive(h, "X-Custom-Secret", "X-Another-Secret")
	h.Set("X-Custom-Secret", "x")
	h.Set("X-Another-Secret", "x")

	stripCredentialHeaders(h)

	for _, name := range []string{
		"Authorization", "Cookie", "X-Api-Key", "Apikey",
		"X-Amz-Security-Token", "X-Custom-Secret", "X-Another-Secret",
		HeaderSensitiveNames,
	} {
		if v := h.Get(name); v != "" {
			t.Errorf("%s survived stripping: %q", name, v)
		}
	}
	if h.Get("Content-Type") != "application/json" {
		t.Error("Content-Type must not be stripped")
	}
}

func TestMarkSensitive_Accumulates(t *testing.T) {
	h := http.Header{}
	MarkSensitive(h, "A")
	MarkSensitive(h, "B", "C")
	h.Set("A", "1")
	h.Set("B", "2")
	h.Set("C", "3")

	stripCredentialHeaders(h)

	for _, name := range []string{"A", "B", "C"} {
		if v := h.Get(name); v != "" {
			t.Errorf("header %s survived after two MarkSensitive calls: %q", name, v)
		}
	}
}

func TestMarkSensitive_EmptyIsNoop(t *testing.T) {
	h := http.Header{}
	MarkSensitive(h)
	if _, ok := h[http.CanonicalHeaderKey(HeaderSensitiveNames)]; ok {
		t.Error("MarkSensitive with no names set the marker header")
	}
}
