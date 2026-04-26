# MiniSMS

MiniSMS is a Go-based SMS middleware gateway with a client-facing REST API, server-rendered Admin UI, routing/failover, prepaid billing, and DLR webhook forwarding.

## Repository Layout

- `minisms/` - main Go application source code
- `doc/` - product, API, admin, DevOps, release, and bootstrap documentation
- `certs/` - local development TLS certificates
- `bin/` - locally built helper binaries

## Main Application

The application code, Makefile, migrations, templates, and deploy artifacts are under:

- `minisms/`

Quick commands:

```bash
cd minisms
go build ./...
go test ./...
make run
```

## Documentation

Primary docs:

- `doc/MiniSMS_Product_Documentation.md`
- `doc/MiniSMS_API_Guide.md`
- `doc/MiniSMS_Admin_Guide.md`
- `doc/MiniSMS_DevOps_Guide.md`
- `doc/MiniSMS_Bootstrap_Prompt.md`
- `doc/MiniSMS_Bootstrap_Prompt_Short.md`

