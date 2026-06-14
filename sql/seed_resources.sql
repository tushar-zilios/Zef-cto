-- seed_resources.sql
-- Idempotent seed for CTO-module resources.
-- Run against the Zef PostgreSQL database; ON CONFLICT DO NOTHING makes it safe to re-run.
--
-- Resource hierarchy:
--   vault
--   └── vault.credentials  (parent_id → vault)
--   audit_logs

-- ── Resources ────────────────────────────────────────────────────────────────
-- vault and audit_logs are roots (parent_id = NULL).
-- vault.credentials is a child of vault.

INSERT INTO public.resources (resource_id, name, parent_id) VALUES
    ('00000005-0000-0000-0000-000000000001', 'vault',             NULL),
    ('00000005-0000-0000-0000-000000000003', 'audit_logs',        NULL),
    ('00000005-0000-0000-0000-000000000002', 'vault.credentials', '00000005-0000-0000-0000-000000000001')
ON CONFLICT (resource_id) DO NOTHING;
