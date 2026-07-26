package dispatcher

import "strings"

// applyResponseMapping is ResponseShaper from ARCHITECTURE.md's component
// list. A tool's response_mapping is a small, declarative reshape recipe —
// no scripting language, just dot-path selection/renaming, since the whole
// point of the pipeline is that the shape was decided at config time, not
// computed by logic at dispatch time.
//
//	{"select": "data.items"}
//	{"rename": {"orderId": "data.order.id", "status": "data.order.state"}}
//
// An empty/nil mapping returns body unchanged.
func applyResponseMapping(mapping map[string]any, body any) any {
	if len(mapping) == 0 {
		return body
	}

	if selectPath, ok := mapping["select"].(string); ok && selectPath != "" {
		if v, found := dotGet(body, selectPath); found {
			body = v
		}
	}

	if rename, ok := mapping["rename"].(map[string]any); ok {
		shaped := make(map[string]any, len(rename))
		for newKey, pathVal := range rename {
			path, _ := pathVal.(string)
			if v, found := dotGet(body, path); found {
				shaped[newKey] = v
			}
		}
		return shaped
	}

	return body
}

func dotGet(body any, path string) (any, bool) {
	if path == "" {
		return body, true
	}
	current := body
	for _, segment := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
