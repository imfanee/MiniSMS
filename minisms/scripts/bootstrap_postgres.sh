#!/usr/bin/env bash
# Creates the minisms role and database (default password: password, matches .env.example).
#
#   cd minisms && ./scripts/bootstrap_postgres.sh
# You will be prompted for sudo once unless you run as the postgres system user, e.g.:
#   sudo -u postgres ./scripts/bootstrap_postgres.sh
set -euo pipefail
PSQL=(psql -v ON_ERROR_STOP=1)
CREATEDB=(createdb -O minisms)

runas_postgres() {
  if [[ "$(id -un)" == "postgres" ]]; then
    "$@"
  else
    sudo -u postgres "$@"
  fi
}

runas_postgres "${PSQL[@]}" <<'SQL'
DO $body$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'minisms') THEN
    CREATE ROLE minisms WITH LOGIN PASSWORD 'password';
  END IF;
END
$body$;
SQL

if ! runas_postgres "${PSQL[@]}" -tAc "SELECT 1 FROM pg_database WHERE datname = 'minisms'" | grep -q 1; then
  runas_postgres "${CREATEDB[@]}" minisms
fi

echo "OK: role and database 'minisms' are ready."
echo "DATABASE_URL=postgres://minisms:password@127.0.0.1:5432/minisms?sslmode=disable"
