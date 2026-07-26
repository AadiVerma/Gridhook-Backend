package parsers

import (
	"fmt"
	"net/url"
	"strings"

	"gridhook.dev/connector-backend/internal/models"
)

// CurlParser drafts a single tool from one pasted curl command — the
// simplest and most common way a user hands over "here's the one endpoint I
// need." It recognizes -X/--request, -H/--header (folded into static
// headers), and a bare trailing URL; it does not attempt a full shell-arg
// parse (quoting edge cases, multi-line `\` continuations with embedded
// data payloads) — good enough for the common single-line case, with the
// visual tool-mapping editor as the fallback for anything hairier.
type CurlParser struct{}

func (p *CurlParser) Parse(raw []byte) (*ParseResult, error) {
	fields := tokenize(string(raw))
	if len(fields) == 0 || fields[0] != "curl" {
		return nil, fmt.Errorf("parsers: curl: input does not start with 'curl'")
	}

	method := models.MethodGET
	headers := map[string]any{}
	var rawURL string

	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "-X", "--request":
			if i+1 < len(fields) {
				method = models.HTTPMethod(strings.ToUpper(fields[i+1]))
				i++
			}
		case "-H", "--header":
			if i+1 < len(fields) {
				parts := strings.SplitN(fields[i+1], ":", 2)
				if len(parts) == 2 {
					headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
				i++
			}
		default:
			if strings.HasPrefix(fields[i], "http://") || strings.HasPrefix(fields[i], "https://") {
				rawURL = fields[i]
			}
		}
	}
	if rawURL == "" {
		return nil, fmt.Errorf("parsers: curl: no URL found in command")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsers: curl: invalid URL: %w", err)
	}

	var queryParams []any
	for key := range parsed.Query() {
		queryParams = append(queryParams, key)
	}

	return &ParseResult{
		EngineType: models.EngineREST,
		BaseURL:    parsed.Scheme + "://" + parsed.Host,
		Tools: []DraftTool{{
			Name:   sanitizeName(strings.Trim(parsed.Path, "/")),
			Method: method,
			Path:   parsed.Path,
			EndpointMapping: map[string]any{
				"headers":     headers,
				"queryParams": queryParams,
			},
		}},
	}, nil
}

// tokenize is a minimal shell-word splitter: handles quoted strings, not
// full POSIX shell semantics (backslash escapes inside quotes, etc).
func tokenize(s string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\n' || r == '\t' || r == '\\':
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}
