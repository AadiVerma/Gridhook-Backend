package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/idcodec"
)

var idFields = map[string]bool{
	"id":             true,
	"organizationId": true,
	"companyId":      true,
	"tenantId":       true,
	"userId":         true,
	"connectorId":    true,
	"connectorApiId": true,
	"groupId":        true,
	"mcpServerId":    true,
	"toolId":         true,

	"tool":      true,
	"connector": true,
	"server":    true,

	"connectorIds": true,
	"toolGroupIds": true,
	"toolIds":      true,
}

var opaqueSubtrees = map[string]bool{
	"input":           true,
	"output":          true,
	"parameters":      true,
	"endpointMapping": true,
	"responseMapping": true,
	"outputSchema":    true,
	"metaData":        true,
	"headers":         true,
}

const importPathSuffix = "/connectors/import"

const maxTranslateDepth = 64

func TranslateIDs(codec *idcodec.Codec, logger *slog.Logger) func(http.Handler) http.Handler {
	t := &idTranslator{codec: codec, logger: logger}
	return t.middleware
}

type idTranslator struct {
	codec  *idcodec.Codec
	logger *slog.Logger
}

func (t *idTranslator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exempt := strings.HasSuffix(r.URL.Path, importPathSuffix)

		if !exempt {
			if err := t.decodeRequest(r); err != nil {
				apiError(w, r, http.StatusBadRequest, "invalid_id", err.Error())
				return
			}
		}

		rec := &bufferingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		t.flush(w, rec)
	})
}

func (t *idTranslator) decodeRequest(r *http.Request) error {
	t.decodePath(r)
	if err := t.decodeQuery(r); err != nil {
		return err
	}
	return t.decodeBody(r)
}

func (t *idTranslator) decodePath(r *http.Request) {

	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePath != "" {
		if rewritten, changed := t.rewriteIDSegments(rctx.RoutePath); changed {
			rctx.RoutePath = rewritten
		}
	}

	rewritten, changed := t.rewriteIDSegments(r.URL.Path)
	if !changed {
		return
	}
	r.URL.Path = rewritten
	r.URL.RawPath = ""
	if r.RequestURI != "" {
		if _, query, found := strings.Cut(r.RequestURI, "?"); found {
			r.RequestURI = rewritten + "?" + query
		} else {
			r.RequestURI = rewritten
		}
	}
}

func (t *idTranslator) rewriteIDSegments(path string) (string, bool) {
	if !strings.Contains(path, "/") {
		return path, false
	}

	segments := strings.Split(path, "/")
	changed := false
	for i, seg := range segments {
		if !idcodec.Looks(seg) {
			continue
		}
		id, err := t.codec.Decode(seg)
		if err != nil {
			continue
		}
		segments[i] = fmt.Sprint(id)
		changed = true
	}
	if !changed {
		return path, false
	}
	return strings.Join(segments, "/"), true
}

func (t *idTranslator) decodeQuery(r *http.Request) error {
	query := r.URL.Query()
	if len(query) == 0 {
		return nil
	}

	changed := false
	for key, values := range query {
		for i, v := range values {
			if !idcodec.Looks(v) {
				continue
			}
			id, err := t.codec.Decode(v)
			if err != nil {

				if idFields[key] {
					return fmt.Errorf("query parameter %q is not a valid id", key)
				}
				continue
			}
			values[i] = fmt.Sprint(id)
			changed = true
		}
	}
	if changed {
		r.URL.RawQuery = query.Encode()
	}
	return nil
}

func (t *idTranslator) decodeBody(r *http.Request) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		return nil
	}

	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return fmt.Errorf("could not read request body")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}

	decoded, err := unmarshalPreservingInts(raw)
	if err != nil {

		r.Body = io.NopCloser(bytes.NewReader(raw))
		return nil //nolint:nilerr // the handler reports this with better context
	}

	converted, err := t.decodeTree(decoded, 0)
	if err != nil {
		return err
	}

	rewritten, err := json.Marshal(converted)
	if err != nil {
		return fmt.Errorf("could not rewrite request body")
	}
	r.Body = io.NopCloser(bytes.NewReader(rewritten))
	r.ContentLength = int64(len(rewritten))
	return nil
}

func (t *idTranslator) flush(w http.ResponseWriter, rec *bufferingWriter) {
	body := rec.buf.Bytes()

	if len(body) == 0 || !isJSON(w.Header().Get("Content-Type")) {

		writeBuffered(w, rec.status, body)
		return
	}

	decoded, err := unmarshalPreservingInts(body)
	if err != nil {
		writeBuffered(w, rec.status, body)
		return
	}

	rewritten, err := json.Marshal(t.encodeTree(decoded, 0))
	if err != nil {
		t.logger.Error("idcodec: could not re-encode response", slog.Any("error", err))
		writeBuffered(w, rec.status, body)
		return
	}
	writeBuffered(w, rec.status, rewritten)
}

func writeBuffered(w http.ResponseWriter, status int, body []byte) {

	if len(body) > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	} else {
		w.Header().Del("Content-Length")
	}
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

func (t *idTranslator) encodeTree(v any, depth int) any {
	if depth > maxTranslateDepth {
		return v
	}

	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			switch {
			case opaqueSubtrees[key]:
				out[key] = value
			case idFields[key]:
				out[key] = t.encodeValue(value, depth+1)
			default:
				out[key] = t.encodeTree(value, depth+1)
			}
		}
		return out

	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = t.encodeTree(item, depth+1)
		}
		return out

	default:
		return v
	}
}

func (t *idTranslator) encodeValue(v any, depth int) any {
	switch typed := v.(type) {
	case nil:
		return nil

	case json.Number:
		id, err := typed.Int64()
		if err != nil {
			return v
		}
		if id == 0 {
			return nil
		}
		return t.codec.Encode(id)

	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = t.encodeValue(item, depth+1)
		}
		return out

	default:

		return t.encodeTree(v, depth+1)
	}
}

func (t *idTranslator) decodeTree(v any, depth int) (any, error) {
	if depth > maxTranslateDepth {
		return v, nil
	}

	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			switch {
			case opaqueSubtrees[key]:
				out[key] = value
			case idFields[key]:
				converted, err := t.decodeValue(key, value)
				if err != nil {
					return nil, err
				}
				out[key] = converted
			default:
				converted, err := t.decodeTree(value, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = converted
			}
		}
		return out, nil

	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			converted, err := t.decodeTree(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil

	default:
		return v, nil
	}
}

func (t *idTranslator) decodeValue(key string, v any) (any, error) {
	switch typed := v.(type) {
	case nil:
		return nil, nil

	case string:
		if typed == "" {
			return nil, nil
		}
		id, err := t.codec.Decode(typed)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid id", key)
		}
		return id, nil

	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			converted, err := t.decodeValue(key, item)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil

	case json.Number:

		return nil, fmt.Errorf("%q must be an opaque id, not a number", key)

	default:

		return t.decodeTree(v, 0)
	}
}

func unmarshalPreservingInts(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var out any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func isJSON(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/json")
}

type bufferingWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	status      int
	wroteHeader bool
}

func (w *bufferingWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
}

func (w *bufferingWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.buf.Write(b)
}
