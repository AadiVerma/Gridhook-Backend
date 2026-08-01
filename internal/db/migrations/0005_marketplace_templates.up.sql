-- Marketplace: a global, admin-curated catalog of install-able adapter
-- templates. Distinct from `connectors`, which is always an org-owned,
-- already-live instance — a template is just a bundled spec + metadata that
-- gets cloned into an org's connectors/connector_apis/mcp_tools via the same
-- parser pipeline `POST /connectors/import` already uses.
--
-- v1 only covers vendors whose API is a direct fit for that pipeline (a
-- static, tenant-agnostic OpenAPI spec): Stripe, Twilio, Jira, Google
-- Calendar. Salesforce/SAP OData/PostgreSQL/Workday/Zendesk need live
-- per-org introspection instead of a fixed spec and are a separate follow-up.

CREATE TYPE adapter_category AS ENUM (
    'crm', 'dev_tools', 'payments', 'communications', 'database',
    'productivity', 'erp', 'support', 'hr', 'commerce'
);

CREATE TABLE adapter_templates (
    id            BIGSERIAL PRIMARY KEY,
    key           TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    glyph         TEXT,
    category      adapter_category NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    engine_type   engine_type NOT NULL,
    auth_type     auth_type NOT NULL,
    base_url      TEXT NOT NULL,
    spec_format   TEXT NOT NULL DEFAULT 'openapi',
    spec_raw      JSONB NOT NULL,
    -- Cached count of operations in spec_raw, set once at seed time. The
    -- spec never changes post-seed, so this avoids re-parsing on every list.
    tool_count    INTEGER NOT NULL DEFAULT 0,
    install_count BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO adapter_templates (key, name, glyph, category, description, engine_type, auth_type, base_url, spec_format, tool_count, spec_raw)
VALUES (
    'stripe', 'Stripe', 'S', 'payments',
    'Charges, subscriptions, invoices, and payouts as callable tools.',
    'REST', 'bearer', 'https://api.stripe.com/v1', 'openapi', 14,
$spec$
{
  "servers": [{"url": "https://api.stripe.com/v1"}],
  "paths": {
    "/charges": {
      "post": {"operationId": "createCharge", "summary": "Create a charge", "description": "Charge a payment source for a given amount.", "requestBody": {"required": true}},
      "get": {"operationId": "listCharges", "summary": "List charges", "description": "List all charges, optionally filtered by customer.", "parameters": [
        {"name": "customer", "in": "query", "required": false, "schema": {"type": "string"}},
        {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer"}}
      ]}
    },
    "/charges/{charge}": {
      "get": {"operationId": "retrieveCharge", "summary": "Retrieve a charge", "description": "Retrieve the details of a previously created charge.", "parameters": [
        {"name": "charge", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/customers": {
      "post": {"operationId": "createCustomer", "summary": "Create a customer", "description": "Create a new customer object.", "requestBody": {"required": true}}
    },
    "/customers/{customer}": {
      "get": {"operationId": "retrieveCustomer", "summary": "Retrieve a customer", "description": "Retrieve a customer by ID.", "parameters": [
        {"name": "customer", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/subscriptions": {
      "post": {"operationId": "createSubscription", "summary": "Create a subscription", "description": "Create a subscription for a customer.", "requestBody": {"required": true}}
    },
    "/subscriptions/{subscription}": {
      "get": {"operationId": "retrieveSubscription", "summary": "Retrieve a subscription", "parameters": [
        {"name": "subscription", "in": "path", "required": true, "schema": {"type": "string"}}
      ]},
      "post": {"operationId": "updateSubscription", "summary": "Update a subscription", "parameters": [
        {"name": "subscription", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "delete": {"operationId": "cancelSubscription", "summary": "Cancel a subscription", "parameters": [
        {"name": "subscription", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/invoices": {
      "post": {"operationId": "createInvoice", "summary": "Create an invoice", "requestBody": {"required": true}},
      "get": {"operationId": "listInvoices", "summary": "List invoices", "parameters": [
        {"name": "customer", "in": "query", "required": false, "schema": {"type": "string"}}
      ]}
    },
    "/invoices/{invoice}": {
      "get": {"operationId": "retrieveInvoice", "summary": "Retrieve an invoice", "parameters": [
        {"name": "invoice", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/payouts": {
      "get": {"operationId": "listPayouts", "summary": "List payouts", "parameters": [
        {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer"}}
      ]}
    },
    "/payouts/{payout}": {
      "get": {"operationId": "retrievePayout", "summary": "Retrieve a payout", "parameters": [
        {"name": "payout", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    }
  }
}
$spec$::jsonb
);

INSERT INTO adapter_templates (key, name, glyph, category, description, engine_type, auth_type, base_url, spec_format, tool_count, spec_raw)
VALUES (
    'twilio', 'Twilio', 'TW', 'communications',
    'Send SMS, place voice calls, and manage phone numbers.',
    'REST', 'api_key', 'https://api.twilio.com/2010-04-01', 'openapi', 9,
$spec$
{
  "servers": [{"url": "https://api.twilio.com/2010-04-01"}],
  "paths": {
    "/Accounts/{AccountSid}/Messages.json": {
      "post": {"operationId": "sendSms", "summary": "Send an SMS", "description": "Send a text message from a Twilio number.", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "get": {"operationId": "listMessages", "summary": "List messages", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "To", "in": "query", "required": false, "schema": {"type": "string"}},
        {"name": "From", "in": "query", "required": false, "schema": {"type": "string"}}
      ]}
    },
    "/Accounts/{AccountSid}/Messages/{Sid}.json": {
      "get": {"operationId": "getMessage", "summary": "Retrieve a message", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "Sid", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/Accounts/{AccountSid}/Calls.json": {
      "post": {"operationId": "createCall", "summary": "Place a voice call", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "get": {"operationId": "listCalls", "summary": "List calls", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "Status", "in": "query", "required": false, "schema": {"type": "string"}}
      ]}
    },
    "/Accounts/{AccountSid}/Calls/{Sid}.json": {
      "get": {"operationId": "getCall", "summary": "Retrieve a call", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "Sid", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/Accounts/{AccountSid}/IncomingPhoneNumbers.json": {
      "get": {"operationId": "listPhoneNumbers", "summary": "List phone numbers", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}}
      ]},
      "post": {"operationId": "buyPhoneNumber", "summary": "Purchase a phone number", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}}
    },
    "/Accounts/{AccountSid}/IncomingPhoneNumbers/{Sid}.json": {
      "post": {"operationId": "updatePhoneNumber", "summary": "Update a phone number's configuration", "parameters": [
        {"name": "AccountSid", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "Sid", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}}
    }
  }
}
$spec$::jsonb
);

INSERT INTO adapter_templates (key, name, glyph, category, description, engine_type, auth_type, base_url, spec_format, tool_count, spec_raw)
VALUES (
    'jira', 'Jira', 'JR', 'dev_tools',
    'Create, transition, and query issues across projects and boards.',
    'REST', 'oauth2', 'https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3', 'openapi', 19,
$spec$
{
  "servers": [{"url": "https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3"}],
  "paths": {
    "/issue": {
      "post": {"operationId": "createIssue", "summary": "Create an issue", "requestBody": {"required": true}}
    },
    "/issue/{issueIdOrKey}": {
      "get": {"operationId": "getIssue", "summary": "Get an issue", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ]},
      "put": {"operationId": "updateIssue", "summary": "Update an issue", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "delete": {"operationId": "deleteIssue", "summary": "Delete an issue", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/issue/{issueIdOrKey}/transitions": {
      "post": {"operationId": "transitionIssue", "summary": "Transition an issue", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "get": {"operationId": "listTransitions", "summary": "List available transitions", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/issue/{issueIdOrKey}/comment": {
      "post": {"operationId": "addComment", "summary": "Add a comment", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "get": {"operationId": "listComments", "summary": "List comments", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/issue/{issueIdOrKey}/assignee": {
      "post": {"operationId": "assignIssue", "summary": "Assign an issue", "parameters": [
        {"name": "issueIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}}
    },
    "/search": {
      "post": {"operationId": "searchIssues", "summary": "Search issues with JQL", "requestBody": {"required": true}}
    },
    "/project": {
      "get": {"operationId": "listProjects", "summary": "List projects"},
      "post": {"operationId": "createProject", "summary": "Create a project", "requestBody": {"required": true}}
    },
    "/project/{projectIdOrKey}": {
      "get": {"operationId": "getProject", "summary": "Get a project", "parameters": [
        {"name": "projectIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/project/{projectIdOrKey}/statuses": {
      "get": {"operationId": "getProjectStatuses", "summary": "Get a project's issue type statuses", "parameters": [
        {"name": "projectIdOrKey", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/board": {
      "get": {"operationId": "listBoards", "summary": "List boards"}
    },
    "/board/{boardId}": {
      "get": {"operationId": "getBoard", "summary": "Get a board", "parameters": [
        {"name": "boardId", "in": "path", "required": true, "schema": {"type": "integer"}}
      ]}
    },
    "/board/{boardId}/sprint": {
      "get": {"operationId": "listSprints", "summary": "List sprints on a board", "parameters": [
        {"name": "boardId", "in": "path", "required": true, "schema": {"type": "integer"}}
      ]}
    },
    "/sprint": {
      "post": {"operationId": "createSprint", "summary": "Create a sprint", "requestBody": {"required": true}}
    },
    "/sprint/{sprintId}": {
      "get": {"operationId": "getSprint", "summary": "Get a sprint", "parameters": [
        {"name": "sprintId", "in": "path", "required": true, "schema": {"type": "integer"}}
      ]}
    }
  }
}
$spec$::jsonb
);

INSERT INTO adapter_templates (key, name, glyph, category, description, engine_type, auth_type, base_url, spec_format, tool_count, spec_raw)
VALUES (
    'google-calendar', 'Google Calendar', 'GC', 'productivity',
    'Read and schedule events across shared calendars.',
    'REST', 'oauth2', 'https://www.googleapis.com/calendar/v3', 'openapi', 8,
$spec$
{
  "servers": [{"url": "https://www.googleapis.com/calendar/v3"}],
  "paths": {
    "/calendars/{calendarId}": {
      "get": {"operationId": "getCalendar", "summary": "Get calendar metadata", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/users/me/calendarList": {
      "get": {"operationId": "listCalendarList", "summary": "List calendars on the user's list"}
    },
    "/calendars/{calendarId}/events": {
      "get": {"operationId": "listEvents", "summary": "List events", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "timeMin", "in": "query", "required": false, "schema": {"type": "string"}},
        {"name": "timeMax", "in": "query", "required": false, "schema": {"type": "string"}}
      ]},
      "post": {"operationId": "createEvent", "summary": "Create an event", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}}
    },
    "/calendars/{calendarId}/events/{eventId}": {
      "get": {"operationId": "getEvent", "summary": "Get an event", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "eventId", "in": "path", "required": true, "schema": {"type": "string"}}
      ]},
      "put": {"operationId": "updateEvent", "summary": "Update an event", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "eventId", "in": "path", "required": true, "schema": {"type": "string"}}
      ], "requestBody": {"required": true}},
      "delete": {"operationId": "deleteEvent", "summary": "Delete an event", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "eventId", "in": "path", "required": true, "schema": {"type": "string"}}
      ]}
    },
    "/calendars/{calendarId}/events/quickAdd": {
      "post": {"operationId": "quickAddEvent", "summary": "Quick-add an event from free text", "parameters": [
        {"name": "calendarId", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "text", "in": "query", "required": true, "schema": {"type": "string"}}
      ]}
    }
  }
}
$spec$::jsonb
);
