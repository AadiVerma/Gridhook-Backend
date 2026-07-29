package parsers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"gridhook.dev/connector-backend/internal/models"
)

type GraphQLSDLParser struct{}

func (p *GraphQLSDLParser) Parse(raw []byte) (*ParseResult, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return parseGraphQLIntrospection(raw)
	}
	return parseGraphQLSDL(raw)
}

func parseGraphQLSDL(raw []byte) (*ParseResult, error) {
	schema, err := gqlparser.LoadSchema(&ast.Source{Input: string(raw)})
	if err != nil {
		return nil, fmt.Errorf("parsers: graphql-sdl: invalid schema: %w", err)
	}

	result := &ParseResult{EngineType: models.EngineGraphQL}
	if schema.Query != nil {
		for _, f := range schema.Query.Fields {
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			result.Tools = append(result.Tools, sdlFieldToDraftTool("query", f, schema))
		}
	}
	if schema.Mutation != nil {
		for _, f := range schema.Mutation.Fields {
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			result.Tools = append(result.Tools, sdlFieldToDraftTool("mutation", f, schema))
		}
	}
	return result, nil
}

func sdlFieldToDraftTool(opType string, field *ast.FieldDefinition, schema *ast.Schema) DraftTool {
	return DraftTool{
		Name:        field.Name,
		Method:      models.MethodPOST,
		Description: field.Description,
		Parameters:  sdlArgsToJSONSchema(field.Arguments, schema),
		EndpointMapping: map[string]any{
			"operationName": field.Name,
			"query":         sdlBuildQuery(opType, field, schema),
			"returnType":    field.Type.String(),
			"arguments":     sdlArgsToStructured(field.Arguments, schema),
		},
	}
}

func sdlArgsToStructured(args ast.ArgumentDefinitionList, schema *ast.Schema) []any {
	out := make([]any, 0, len(args))
	for _, arg := range args {
		out = append(out, map[string]any{
			"name":        arg.Name,
			"graphqlType": arg.Type.String(),
			"jsonType":    sdlTypeToJSONSchema(arg.Type, schema, 0)["type"],
			"required":    arg.Type.NonNull,
		})
	}
	return out
}

func sdlArgsToJSONSchema(args ast.ArgumentDefinitionList, schema *ast.Schema) map[string]any {
	properties := map[string]any{}
	required := make([]any, 0, len(args))
	for _, arg := range args {
		properties[arg.Name] = sdlTypeToJSONSchema(arg.Type, schema, 0)
		if arg.Type.NonNull {
			required = append(required, arg.Name)
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func sdlTypeToJSONSchema(t *ast.Type, schema *ast.Schema, depth int) map[string]any {
	if t.Elem != nil {
		return map[string]any{"type": "array", "items": sdlTypeToJSONSchema(t.Elem, schema, depth)}
	}

	if jsonType, ok := scalarJSONType(t.NamedType); ok {
		return map[string]any{"type": jsonType}
	}

	def := schema.Types[t.NamedType]
	if def == nil {
		return map[string]any{"type": "string"}
	}
	switch def.Kind {
	case ast.Enum:
		values := make([]any, 0, len(def.EnumValues))
		for _, v := range def.EnumValues {
			values = append(values, v.Name)
		}
		return map[string]any{"type": "string", "enum": values}
	case ast.InputObject:
		if depth >= 1 {
			return map[string]any{"type": "object"}
		}
		properties := map[string]any{}
		required := make([]any, 0, len(def.Fields))
		for _, f := range def.Fields {
			properties[f.Name] = sdlTypeToJSONSchema(f.Type, schema, depth+1)
			if f.Type.NonNull {
				required = append(required, f.Name)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required}
	default:
		return map[string]any{"type": "string"}
	}
}

func sdlBuildQuery(opType string, field *ast.FieldDefinition, schema *ast.Schema) string {
	varDecls := make([]string, 0, len(field.Arguments))
	argUses := make([]string, 0, len(field.Arguments))
	for _, arg := range field.Arguments {
		varDecls = append(varDecls, fmt.Sprintf("$%s: %s", arg.Name, arg.Type.String()))
		argUses = append(argUses, fmt.Sprintf("%s: $%s", arg.Name, arg.Name))
	}

	varPart, argPart := "", ""
	if len(varDecls) > 0 {
		varPart = "(" + strings.Join(varDecls, ", ") + ")"
		argPart = "(" + strings.Join(argUses, ", ") + ")"
	}

	return fmt.Sprintf("%s %s%s {\n  %s%s%s\n}", opType, field.Name, varPart, field.Name, argPart, sdlSelectionSet(field.Type, schema))
}

func sdlSelectionSet(t *ast.Type, schema *ast.Schema) string {
	name := sdlNamedTypeOf(t)
	if _, ok := scalarJSONType(name); ok {
		return ""
	}
	def := schema.Types[name]
	if def == nil || def.Kind == ast.Scalar || def.Kind == ast.Enum {
		return ""
	}

	var fields []string
	for _, f := range def.Fields {
		fieldName := sdlNamedTypeOf(f.Type)
		if _, ok := scalarJSONType(fieldName); ok {
			fields = append(fields, f.Name)
			continue
		}
		if fd := schema.Types[fieldName]; fd != nil && (fd.Kind == ast.Scalar || fd.Kind == ast.Enum) {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) == 0 {
		fields = []string{"__typename"}
	}
	return " { " + strings.Join(fields, " ") + " }"
}

func sdlNamedTypeOf(t *ast.Type) string {
	for t.Elem != nil {
		t = t.Elem
	}
	return t.NamedType
}

func scalarJSONType(name string) (string, bool) {
	switch name {
	case "String", "ID":
		return "string", true
	case "Int":
		return "integer", true
	case "Float":
		return "number", true
	case "Boolean":
		return "boolean", true
	default:
		return "", false
	}
}

type introspectionEnvelope struct {
	Data *struct {
		Schema introspectionSchema `json:"__schema"`
	} `json:"data"`
	Schema *introspectionSchema `json:"__schema"`
}

type introspectionSchema struct {
	QueryType    *introspectionNamedRef `json:"queryType"`
	MutationType *introspectionNamedRef `json:"mutationType"`
	Types        []introspectionType    `json:"types"`
}

type introspectionNamedRef struct {
	Name string `json:"name"`
}

type introspectionType struct {
	Kind        string                    `json:"kind"`
	Name        string                    `json:"name"`
	Fields      []introspectionField      `json:"fields"`
	EnumValues  []introspectionNamedRef   `json:"enumValues"`
	InputFields []introspectionInputValue `json:"inputFields"`
}

type introspectionField struct {
	Name string                    `json:"name"`
	Args []introspectionInputValue `json:"args"`
	Type introspectionTypeRef      `json:"type"`
}

type introspectionInputValue struct {
	Name string               `json:"name"`
	Type introspectionTypeRef `json:"type"`
}

type introspectionTypeRef struct {
	Kind   string                `json:"kind"`
	Name   string                `json:"name"`
	OfType *introspectionTypeRef `json:"ofType"`
}

func parseGraphQLIntrospection(raw []byte) (*ParseResult, error) {
	var env introspectionEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsers: graphql-sdl: invalid introspection JSON: %w", err)
	}
	schema := env.Schema
	if schema == nil && env.Data != nil {
		schema = &env.Data.Schema
	}
	if schema == nil {
		return nil, fmt.Errorf("parsers: graphql-sdl: introspection JSON missing __schema")
	}

	types := make(map[string]introspectionType, len(schema.Types))
	for _, t := range schema.Types {
		types[t.Name] = t
	}

	result := &ParseResult{EngineType: models.EngineGraphQL}
	if schema.QueryType != nil {
		if def, ok := types[schema.QueryType.Name]; ok {
			for _, f := range def.Fields {
				result.Tools = append(result.Tools, introspectionFieldToDraftTool("query", f, types))
			}
		}
	}
	if schema.MutationType != nil {
		if def, ok := types[schema.MutationType.Name]; ok {
			for _, f := range def.Fields {
				result.Tools = append(result.Tools, introspectionFieldToDraftTool("mutation", f, types))
			}
		}
	}
	return result, nil
}

func introspectionFieldToDraftTool(opType string, field introspectionField, types map[string]introspectionType) DraftTool {
	return DraftTool{
		Name:       field.Name,
		Method:     models.MethodPOST,
		Parameters: introspectionArgsToJSONSchema(field.Args, types),
		EndpointMapping: map[string]any{
			"operationName": field.Name,
			"query":         introspectionBuildQuery(opType, field, types),
			"returnType":    introspectionTypeString(field.Type),
			"arguments":     introspectionArgsToStructured(field.Args, types),
		},
	}
}

func introspectionArgsToStructured(args []introspectionInputValue, types map[string]introspectionType) []any {
	out := make([]any, 0, len(args))
	for _, arg := range args {
		out = append(out, map[string]any{
			"name":        arg.Name,
			"graphqlType": introspectionTypeString(arg.Type),
			"jsonType":    introspectionTypeToJSONSchema(arg.Type, types, 0)["type"],
			"required":    arg.Type.Kind == "NON_NULL",
		})
	}
	return out
}

func introspectionArgsToJSONSchema(args []introspectionInputValue, types map[string]introspectionType) map[string]any {
	properties := map[string]any{}
	required := make([]any, 0, len(args))
	for _, arg := range args {
		properties[arg.Name] = introspectionTypeToJSONSchema(arg.Type, types, 0)
		if arg.Type.Kind == "NON_NULL" {
			required = append(required, arg.Name)
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func introspectionTypeToJSONSchema(t introspectionTypeRef, types map[string]introspectionType, depth int) map[string]any {
	switch t.Kind {
	case "NON_NULL", "":
		if t.OfType != nil {
			return introspectionTypeToJSONSchema(*t.OfType, types, depth)
		}
	case "LIST":
		if t.OfType != nil {
			return map[string]any{"type": "array", "items": introspectionTypeToJSONSchema(*t.OfType, types, depth)}
		}
	}

	if jsonType, ok := scalarJSONType(t.Name); ok {
		return map[string]any{"type": jsonType}
	}

	def, ok := types[t.Name]
	if !ok {
		return map[string]any{"type": "string"}
	}
	switch def.Kind {
	case "ENUM":
		values := make([]any, 0, len(def.EnumValues))
		for _, v := range def.EnumValues {
			values = append(values, v.Name)
		}
		return map[string]any{"type": "string", "enum": values}
	case "INPUT_OBJECT":
		if depth >= 1 {
			return map[string]any{"type": "object"}
		}
		properties := map[string]any{}
		required := make([]any, 0, len(def.InputFields))
		for _, f := range def.InputFields {
			properties[f.Name] = introspectionTypeToJSONSchema(f.Type, types, depth+1)
			if f.Type.Kind == "NON_NULL" {
				required = append(required, f.Name)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required}
	default:
		return map[string]any{"type": "string"}
	}
}

func introspectionBuildQuery(opType string, field introspectionField, types map[string]introspectionType) string {
	varDecls := make([]string, 0, len(field.Args))
	argUses := make([]string, 0, len(field.Args))
	for _, arg := range field.Args {
		varDecls = append(varDecls, fmt.Sprintf("$%s: %s", arg.Name, introspectionTypeString(arg.Type)))
		argUses = append(argUses, fmt.Sprintf("%s: $%s", arg.Name, arg.Name))
	}

	varPart, argPart := "", ""
	if len(varDecls) > 0 {
		varPart = "(" + strings.Join(varDecls, ", ") + ")"
		argPart = "(" + strings.Join(argUses, ", ") + ")"
	}

	return fmt.Sprintf("%s %s%s {\n  %s%s%s\n}", opType, field.Name, varPart, field.Name, argPart, introspectionSelectionSet(field.Type, types))
}

func introspectionTypeString(t introspectionTypeRef) string {
	switch t.Kind {
	case "NON_NULL":
		if t.OfType != nil {
			return introspectionTypeString(*t.OfType) + "!"
		}
	case "LIST":
		if t.OfType != nil {
			return "[" + introspectionTypeString(*t.OfType) + "]"
		}
	}
	return t.Name
}

func introspectionNamedTypeOf(t introspectionTypeRef) string {
	for t.OfType != nil {
		t = *t.OfType
	}
	return t.Name
}

func introspectionSelectionSet(t introspectionTypeRef, types map[string]introspectionType) string {
	name := introspectionNamedTypeOf(t)
	if _, ok := scalarJSONType(name); ok {
		return ""
	}
	def, ok := types[name]
	if !ok || def.Kind == "SCALAR" || def.Kind == "ENUM" {
		return ""
	}

	var fields []string
	for _, f := range def.Fields {
		fieldName := introspectionNamedTypeOf(f.Type)
		if _, ok := scalarJSONType(fieldName); ok {
			fields = append(fields, f.Name)
			continue
		}
		if fd, ok := types[fieldName]; ok && (fd.Kind == "SCALAR" || fd.Kind == "ENUM") {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) == 0 {
		fields = []string{"__typename"}
	}
	return " { " + strings.Join(fields, " ") + " }"
}
