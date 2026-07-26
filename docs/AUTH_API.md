# Auth API — Signup & Login

## The model, in one paragraph

A person is identified by email. Signing up always creates a **new** organization — signup
never auto-joins you into a stranger's org, even if you share an email domain like `gmail.com`.
Joining an *existing* organization only happens via an invite link sent by someone already in
it (not built yet). Because of that invite path, one email can eventually belong to **multiple
organizations** — so **login can be ambiguous**: if it is, the API returns the list of
organizations instead of a session, and you call login again with the chosen one.

<br/>

## `POST /auth/register`

Always creates a **new organization**.

**Request**
```json
{
  "name": "Aditya Verma",
  "email": "aditya@procol.in",
  "password": "correct-horse-battery-staple",
  "organization": "Procol"
}
```
`organization` is optional — if left blank, the org is named after the email's domain.

**Response `201`**
```json
{
  "token": "fNtZfGgYkZe32ZwB_Pv5V8IwRh5tNsmPb1qNOy6CUCk",
  "user": {
    "id": 4,
    "organizationId": 7,
    "email": "aditya@procol.in",
    "name": "Aditya Verma",
    "role": "owner",
    "status": "active",
    "createdAt": "2026-07-26T23:04:43Z"
  }
}
```
`role` is always `"owner"` on register — you own whatever org you just created.

**An email that's already registered anywhere is always rejected** — regardless of password.
The only way to join an existing organization is an invite link from someone already in it
(not built yet). Re-registering is never how you add a second org to your account.

**Error `409 email_taken`**
```json
{ "error": { "code": "email_taken", "message": "an account with this email already exists — log in instead" } }
```
Show this as "you already have an account — want to log in instead?" on the signup form.

<br/>

## `POST /auth/login`

**Request**
```json
{ "email": "aditya@procol.in", "password": "correct-horse-battery-staple" }
```
`organizationId` is optional — omit it on the first attempt.

**Response `200` — single organization (logs in immediately)**
```json
{
  "token": "fNtZfGgYkZe32ZwB_Pv5V8IwRh5tNsmPb1qNOy6CUCk",
  "user": { "id": 4, "organizationId": 7, "email": "...", "name": "...", "role": "owner", "status": "active", "createdAt": "..." }
}
```

**Response `200` — multiple organizations (no session yet — you must choose)**
```json
{
  "organizations": [
    { "id": 7, "name": "Procol", "slug": "procol-9f21ac3d", "role": "owner" },
    { "id": 11, "name": "Side Project Inc", "slug": "side-project-inc-b0dbb885", "role": "owner" }
  ]
}
```
Notice: **no `token` key at all** in this shape — that's how the frontend tells the two
responses apart. Show the list, let the user pick one, then call login again:

```json
{ "email": "aditya@procol.in", "password": "correct-horse-battery-staple", "organizationId": 11 }
```
This time you get the normal `{ "token", "user" }` response. Most users will only ever see the
single-org response — the picker only shows up once someone has joined more than one org via
invite.

**Error `401 invalid_credentials`** — wrong email or password.

**Error `403 not_a_member`** — you passed an `organizationId` the account doesn't actually
belong to (shouldn't happen unless the frontend is caching a stale org list).

<br/>

## `GET /auth/me`

`Authorization: Bearer <token>` → returns the same `user` shape as login/register, for
whichever organization that token's session is bound to. A session is always scoped to one
organization — to work in a different org, log in again with that `organizationId`.

<br/>

## `POST /auth/logout`

`Authorization: Bearer <token>` → revokes that session. `204 No Content`.
