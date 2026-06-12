# Zef-cto

Standalone Go service for the CTO module — runs on port **8081**.

## Environment variables (`.env` in this directory)

| Variable | Required | Notes |
|---|---|---|
| `CTO_DATABASE_URL` | Yes | PostgreSQL connection string for CTO data |
| `JWT_SECRET` | No | Shared with main backend (defaults to dev key) |
| `GEMINI_API_KEY` | No | Enables `/cto/ideate/chat` |
| `CTO_ENCRYPTION_KEY` | No | 32-byte AES key for credential encryption |
| `PORT` | No | Defaults to `8081` |

## Run

```bash
go run ./src/cmd/server/main.go
```

## Build

```bash
go build -o cto-server ./src/cmd/server/main.go
```

## API Routes (all require `Authorization: Bearer <token>`)

```
GET  /health

POST   /cto/ideate/chat
GET    /cto/ideate/history
DELETE /cto/ideate/history

GET    /cto/projects
POST   /cto/projects
GET    /cto/projects/{id}
PUT    /cto/projects/{id}
DELETE /cto/projects/{id}

GET    /cto/projects/{id}/credentials
PUT    /cto/projects/{id}/credentials
DELETE /cto/projects/{id}/credentials
POST   /cto/projects/{id}/test-connection
GET    /cto/projects/{id}/health
GET    /cto/projects/{id}/sql-history
DELETE /cto/projects/{id}/sql-history
GET    /cto/projects/{id}/saved-queries
POST   /cto/projects/{id}/saved-queries
PUT    /cto/projects/{id}/saved-queries/{queryId}
DELETE /cto/projects/{id}/saved-queries/{queryId}
GET    /cto/projects/{id}/snapshots
POST   /cto/projects/{id}/snapshots
GET    /cto/projects/{id}/snapshots/{snapshotId}
DELETE /cto/projects/{id}/snapshots/{snapshotId}

GET  /cto/db/schemas
GET  /cto/db/tables
GET  /cto/db/tables/{schema}/{table}/columns
GET  /cto/db/tables/{schema}/{table}/rows
POST /cto/db/sql
GET  /cto/db/extensions
GET  /cto/db/functions
GET  /cto/db/triggers
GET  /cto/db/roles
GET  /cto/db/views
```
