package parsers

import (
	"strings"
	"testing"

	"gridhook.dev/connector-backend/internal/models"
)

const sampleSDL = `
type Query {
  getUser(id: ID!): User
}

type Mutation {
  createUser(input: CreateUserInput!): User
}

type User {
  id: ID!
  name: String!
  email: String
  posts: [Post!]
}

type Post {
  id: ID!
  title: String!
}

input CreateUserInput {
  name: String!
  email: String
}
`

func findTool(tools []DraftTool, name string) (DraftTool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return DraftTool{}, false
}

func TestGraphQLSDLParser_Parse_SDL(t *testing.T) {
	result, err := (&GraphQLSDLParser{}).Parse([]byte(sampleSDL))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.EngineType != models.EngineGraphQL {
		t.Errorf("EngineType = %q, want GRAPHQL", result.EngineType)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(result.Tools))
	}

	getUser, ok := findTool(result.Tools, "getUser")
	if !ok {
		t.Fatal("getUser tool not found")
	}
	props, _ := getUser.Parameters["properties"].(map[string]any)
	id, _ := props["id"].(map[string]any)
	if id["type"] != "string" {
		t.Errorf("getUser.id type = %v, want string", id["type"])
	}
	required, _ := getUser.Parameters["required"].([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("getUser required = %v, want [id]", required)
	}
	query, _ := getUser.EndpointMapping["query"].(string)
	if !strings.Contains(query, "query getUser($id: ID!)") {
		t.Errorf("getUser query missing variable decl: %s", query)
	}
	if !strings.Contains(query, "getUser(id: $id)") {
		t.Errorf("getUser query missing argument use: %s", query)
	}
	for _, want := range []string{"id", "name", "email"} {
		if !strings.Contains(query, want) {
			t.Errorf("getUser query missing scalar selection %q: %s", want, query)
		}
	}
	if strings.Contains(query, "posts") {
		t.Errorf("getUser query should not select the object-typed posts field: %s", query)
	}

	createUser, ok := findTool(result.Tools, "createUser")
	if !ok {
		t.Fatal("createUser tool not found")
	}
	cuProps, _ := createUser.Parameters["properties"].(map[string]any)
	input, _ := cuProps["input"].(map[string]any)
	if input["type"] != "object" {
		t.Fatalf("createUser.input type = %v, want object", input["type"])
	}
	inputProps, _ := input["properties"].(map[string]any)
	if inputProps["name"].(map[string]any)["type"] != "string" {
		t.Errorf("createUser.input.name type = %v, want string", inputProps["name"])
	}
	inputRequired, _ := input["required"].([]any)
	if len(inputRequired) != 1 || inputRequired[0] != "name" {
		t.Errorf("createUser.input required = %v, want [name]", inputRequired)
	}
	cuRequired, _ := createUser.Parameters["required"].([]any)
	if len(cuRequired) != 1 || cuRequired[0] != "input" {
		t.Errorf("createUser required = %v, want [input]", cuRequired)
	}
}

const sampleIntrospection = `{
  "data": {
    "__schema": {
      "queryType": {"name": "Query"},
      "types": [
        {
          "kind": "OBJECT",
          "name": "Query",
          "fields": [
            {
              "name": "getItem",
              "args": [
                {"name": "id", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}
              ],
              "type": {"kind": "OBJECT", "name": "Item"}
            }
          ]
        },
        {
          "kind": "OBJECT",
          "name": "Item",
          "fields": [
            {"name": "id", "args": [], "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}},
            {"name": "name", "args": [], "type": {"kind": "SCALAR", "name": "String"}},
            {"name": "tags", "args": [], "type": {"kind": "LIST", "ofType": {"kind": "SCALAR", "name": "String"}}}
          ]
        }
      ]
    }
  }
}`

func TestGraphQLSDLParser_Parse_Introspection(t *testing.T) {
	result, err := (&GraphQLSDLParser{}).Parse([]byte(sampleIntrospection))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(result.Tools))
	}

	tool := result.Tools[0]
	if tool.Name != "getItem" {
		t.Errorf("Name = %q, want getItem", tool.Name)
	}
	props, _ := tool.Parameters["properties"].(map[string]any)
	id, _ := props["id"].(map[string]any)
	if id["type"] != "string" {
		t.Errorf("id type = %v, want string", id["type"])
	}
	required, _ := tool.Parameters["required"].([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("required = %v, want [id]", required)
	}

	query, _ := tool.EndpointMapping["query"].(string)
	if !strings.Contains(query, "query getItem($id: ID!)") {
		t.Errorf("query missing variable decl: %s", query)
	}
	for _, want := range []string{"id", "name", "tags"} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing scalar selection %q: %s", want, query)
		}
	}
}

func TestGraphQLSDLParser_Parse_InvalidSDL(t *testing.T) {
	_, err := (&GraphQLSDLParser{}).Parse([]byte("type Query { broken"))
	if err == nil {
		t.Fatal("expected error for malformed SDL, got nil")
	}
}
