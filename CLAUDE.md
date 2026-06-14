# Zef-cto

## DATABASE OWNERSHIP
This service owns **one specific Supabase PostgreSQL database**:
- Project ID: `wgoouuzlsmuxffeicnrn` (ap-southeast-1)
- Env var: `DATABASE_URL` in `Zef-cto/.env`

**DO NOT** read or modify DB schemas/queries from `Zef-backend/` or `Zef-accountant/`. Those are separate services with separate databases.

## This service handles
- CTO / engineering management domain
- Port 8081

Run: `cd Zef-cto && go run cto-server/main.go` (or via root `make run-cto`)
