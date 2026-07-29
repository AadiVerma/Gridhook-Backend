package dispatcher

import (
	"reflect"
	"testing"
)

func TestApplyResponseMapping(t *testing.T) {
	body := map[string]any{
		"data": map[string]any{
			"user": map[string]any{"id": 7.0, "name": "Ada"},
		},
		"meta": map[string]any{"page": 1.0},
	}

	cases := []struct {
		name    string
		mapping map[string]any
		body    any
		want    any
	}{
		{
			name:    "empty mapping is a pass-through",
			mapping: nil,
			body:    body,
			want:    body,
		},
		{
			name:    "select narrows to a nested path",
			mapping: map[string]any{"select": "data.user"},
			body:    body,
			want:    map[string]any{"id": 7.0, "name": "Ada"},
		},
		{
			name:    "select of a missing path leaves the body alone",
			mapping: map[string]any{"select": "data.absent"},
			body:    body,
			want:    body,
		},
		{
			name:    "rename projects fields by path",
			mapping: map[string]any{"rename": map[string]any{"userId": "data.user.id"}},
			body:    body,
			want:    map[string]any{"userId": 7.0},
		},
		{
			name: "select then rename compose",
			mapping: map[string]any{
				"select": "data.user",
				"rename": map[string]any{"identifier": "id"},
			},
			body: body,
			want: map[string]any{"identifier": 7.0},
		},
		{
			name:    "rename skips paths that do not resolve",
			mapping: map[string]any{"rename": map[string]any{"missing": "nope.nope"}},
			body:    body,
			want:    map[string]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyResponseMapping(tc.mapping, tc.body)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("applyResponseMapping() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDotGet(t *testing.T) {
	body := map[string]any{
		"a": map[string]any{"b": map[string]any{"c": "deep"}},
		"n": nil,
	}

	cases := []struct {
		path      string
		want      any
		wantFound bool
	}{
		{"", body, true},
		{"a.b.c", "deep", true},
		{"a.b", map[string]any{"c": "deep"}, true},
		{"a.missing", nil, false},
		{"a.b.c.d", nil, false},
		{"n", nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, found := dotGet(body, tc.path)
			if found != tc.wantFound {
				t.Fatalf("dotGet(%q) found = %v, want %v", tc.path, found, tc.wantFound)
			}
			if found && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("dotGet(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func TestApplyResponseMapping_NonMapBody(t *testing.T) {
	got := applyResponseMapping(map[string]any{"select": "a.b"}, "<xml/>")
	if got != "<xml/>" {
		t.Errorf("applyResponseMapping on a string body = %#v, want it unchanged", got)
	}
}
