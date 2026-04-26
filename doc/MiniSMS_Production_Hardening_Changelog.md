# MiniSMS Production Hardening Changelog

Date: 2026-04-26  
Scope: Final production-readiness hardening, API alignment, security fixes, and documentation synchronization.

## Release Summary

This update finalizes core production hardening tasks across security, API consistency, admin operations, observability metadata, and documentation alignment.

## Code Changes

### Security

- Upgraded `github.com/gorilla/csrf` from `v1.7.2` to `v1.7.3` to address known CSRF vulnerability advisory (`GO-2025-3607`).
- Replaced API key hash comparison with constant-time verification in `internal/db/api_keys.go`.
- Added login throttling in `internal/web/auth.go`:
  - keyed by username + source IP
  - failure window: 10 minutes
  - block threshold: 5 failures
  - temporary block duration: 15 minutes

### API and Data Visibility

- Expanded `POST /api/v1/sms/send` response in `internal/api/sms.go` to include:
  - `sender_id`
  - `sender_id_source`
  - `source_addr_ton`
  - `source_addr_npi`
  - `dest_addr_ton`
  - `dest_addr_npi`
- Expanded `GET /api/v1/sms/status/{message_id}` in `internal/api/account.go` to include:
  - sender/client reference fields
  - carrier name
  - message timeline fields
  - DLR requested/webhook/status/forward metadata
  - DLR forward attempts
  - SMPP TON/NPI values
- Extended SMS log detail modal data/query in `internal/web/sms_logs.go` and `templates/admin/sms_logs/detail_modal.html` to show:
  - SMPP TON/NPI fields
  - `carrier_skip_reason`

### Admin Operations

- Added full Audit Log screen and route:
  - handler: `internal/web/audit_log.go`
  - template: `templates/admin/audit_log/list.html`
  - route: `GET /admin/audit-log`
- Wired audit template into handler bundle and startup template loading in `cmd/minisms/main.go`.

### Runtime/Server

- Increased graceful shutdown timeout from 10s to 30s in `cmd/minisms/main.go`.
- Added runtime health metadata output in `/healthz`:
  - `status`
  - `version`
  - `commit`
  - `build_time`
- Added shared template parser helper with `FuncMap` support (`hasPrefix`) in `cmd/minisms/main.go`.

### Test Improvements

- Expanded template variable test coverage in `internal/carrier/template_test.go` to assert DLR callback and SMPP placeholders.
- Stabilized config required-env test setup in `internal/config/config_test.go` by explicitly clearing required env vars.

## Documentation Synchronization

The following docs were updated to reflect actual runtime behavior:

- `doc/MiniSMS_API_Guide.md`
  - send response fields updated to match current handlers
  - status response fields updated with DLR + SMPP metadata
- `doc/MiniSMS_Product_Documentation.md`
  - security model updated with login throttling
  - health endpoint shape updated with build metadata
- `doc/MiniSMS_DevOps_Guide.md`
  - health response keys documented
  - new troubleshooting case for login temporary blocks (`429`)
  - hardening checklist updated for login failure monitoring
- `doc/MiniSMS_Admin_Guide.md`
  - login section updated with lockout behavior guidance

## Verification Results

Executed after fixes:

- `go build ./...` passed
- `go vet ./...` passed
- `go test ./...` passed
- `govulncheck ./...` passed (no called-code vulnerabilities)

Coverage snapshot:

- Aggregated statement coverage (`go test -coverpkg=./... ./...`): **5.0%**

## Notes

- This changelog captures production hardening and documentation sync only.
- Functional behavior now matches current implementation and updated guides.
