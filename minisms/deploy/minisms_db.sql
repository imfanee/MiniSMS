-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
-- =============================================================================
-- MiniSMS — Complete Database Schema (consolidated)
-- Generated: 2026-06-05
-- PostgreSQL 15+
--
-- Fresh installation:
--   psql "$DATABASE_URL" -f deploy/minisms_db.sql
--   make schema DB_URL="$DATABASE_URL"
--
-- Replaces incremental migrations 001–014. Application no longer auto-migrates
-- on startup; apply this file explicitly when provisioning or upgrading schema.
-- =============================================================================


-- >>> 001_initial_schema.up.sql <<<
-- =============================================================================
-- MiniSMS — PostgreSQL Database Schema
-- Version: 1.1
-- Date:    April 2026
-- =============================================================================
-- Changes in v1.1:
--   [NEW] Carrier financial ledger  — carrier_balance_entries, carriers.balance
--   [NEW] Carrier payment recording — record_carrier_payment() function
--   [NEW] Routing groups            — routing_groups table
--   [NEW] Prefix-based routes       — route_entries (primary + 2 failover carriers)
--   [NEW] Client routing assignment — clients.routing_group_id
--   [NEW] SMS log routing columns   — route_entry_id, failover_sequence
--   [UPD] carriers                  — added balance, currency columns
--   [UPD] clients                   — added routing_group_id column
--   [UPD] sms_logs                  — added routing context columns
--   [UPD] Views                     — routing & carrier financial context added
--   [UPD] Grants                    — updated for new tables/functions
-- =============================================================================
-- Conventions:
--   * All primary keys are UUIDs via gen_random_uuid()
--   * All timestamps are TIMESTAMPTZ (UTC)
--   * All monetary amounts are NUMERIC(18,6) — never FLOAT
--   * Carrier auth header values are AES-256-GCM encrypted at app layer
--   * Audit and ledger tables are append-only (immutability triggers)
--   * Enum-like columns use CHECK constraints
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "citext";


-- =============================================================================
-- SECTION 1: ADMIN SESSIONS
-- =============================================================================

CREATE TABLE admin_sessions (
    session_id      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_token   TEXT        NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip_address      INET,
    user_agent      TEXT,
    is_revoked      BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_admin_sessions_token   ON admin_sessions (session_token);
CREATE INDEX idx_admin_sessions_expires ON admin_sessions (expires_at);


-- =============================================================================
-- SECTION 2: RATE GROUPS
-- =============================================================================

CREATE TABLE rate_groups (
    rate_group_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT        NOT NULL UNIQUE,
    currency        CHAR(3)     NOT NULL,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_rate_groups_currency CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE TABLE rate_entries (
    rate_entry_id   UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    rate_group_id   UUID            NOT NULL REFERENCES rate_groups (rate_group_id) ON DELETE RESTRICT,
    prefix          TEXT            NOT NULL,
    description     TEXT,
    rate_per_sms    NUMERIC(18,6)   NOT NULL CHECK (rate_per_sms >= 0),
    effective_from  DATE            NOT NULL,
    effective_to    DATE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_rate_entries_prefix     CHECK (prefix = '*' OR prefix ~ '^[0-9]+$'),
    CONSTRAINT chk_rate_entries_dates      CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT uq_rate_entries_group_prefix_from UNIQUE (rate_group_id, prefix, effective_from)
);

CREATE INDEX idx_rate_entries_group  ON rate_entries (rate_group_id);
CREATE INDEX idx_rate_entries_prefix ON rate_entries (prefix);


-- =============================================================================
-- SECTION 3: CARRIERS
-- =============================================================================
-- carriers.balance     — current prepaid credit balance with carrier.
--                        Increased by record_carrier_payment(), decreased by
--                        deduct_carrier_balance() on each successful SMS dispatch.
-- carriers.currency    — ISO 4217 currency of the carrier account.
-- carriers.rate_group_id — carrier cost rate group for reconciliation.
-- =============================================================================

CREATE TABLE carriers (
    carrier_id      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT            NOT NULL UNIQUE,
    endpoint_url    TEXT            NOT NULL,
    http_method     TEXT            NOT NULL DEFAULT 'POST',
    status          TEXT            NOT NULL DEFAULT 'active',
    rate_group_id   UUID            REFERENCES rate_groups (rate_group_id) ON DELETE SET NULL,
    balance         NUMERIC(18,6)   NOT NULL DEFAULT 0,
    currency        CHAR(3)         NOT NULL DEFAULT 'GBP',
    notes           TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_carriers_http_method CHECK (http_method IN ('GET', 'POST')),
    CONSTRAINT chk_carriers_status      CHECK (status IN ('active', 'inactive')),
    CONSTRAINT chk_carriers_currency    CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_carriers_status ON carriers (status);


CREATE TABLE carrier_auth_headers (
    header_id        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id       UUID        NOT NULL REFERENCES carriers (carrier_id) ON DELETE CASCADE,
    header_name      TEXT        NOT NULL,
    header_value_enc TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_carrier_auth_headers_name UNIQUE (carrier_id, header_name)
);

CREATE INDEX idx_carrier_auth_headers_carrier ON carrier_auth_headers (carrier_id);


CREATE TABLE carrier_request_templates (
    template_id     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id      UUID        NOT NULL REFERENCES carriers (carrier_id) ON DELETE CASCADE,
    content_type    TEXT        NOT NULL DEFAULT 'application/json',
    body_template   TEXT,
    query_template  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_carrier_request_templates_carrier UNIQUE (carrier_id),
    CONSTRAINT chk_carrier_request_templates_content_type
        CHECK (content_type IN (
            'application/json',
            'application/x-www-form-urlencoded',
            'text/xml',
            'application/xml'
        ))
);

CREATE INDEX idx_carrier_request_templates_carrier ON carrier_request_templates (carrier_id);


CREATE TABLE carrier_usage_totals (
    carrier_id      UUID            PRIMARY KEY REFERENCES carriers (carrier_id) ON DELETE CASCADE,
    total_messages  BIGINT          NOT NULL DEFAULT 0 CHECK (total_messages >= 0),
    total_segments  BIGINT          NOT NULL DEFAULT 0 CHECK (total_segments >= 0),
    total_amount    NUMERIC(18,6)   NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
    last_message_at TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now()
);


-- ---------------------------------------------------------------------------
-- Carrier financial ledger  [NEW in v1.1]
-- ---------------------------------------------------------------------------
-- Append-only record of every financial movement on a carrier account.
--
-- entry_type:
--   payment    — operator pays carrier (prepay); increases balance (+1)
--   charge     — per-SMS cost after dispatch; decreases balance (-1)
--   adjustment — manual admin correction, positive or negative
--   refund     — carrier refunds operator; increases balance (+1)
--
-- amount is always stored as a positive NUMERIC; direction encodes sign.
-- carriers.balance is the live balance, maintained atomically by functions.
-- ---------------------------------------------------------------------------

CREATE TABLE carrier_balance_entries (
    entry_id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id        UUID            NOT NULL REFERENCES carriers (carrier_id) ON DELETE RESTRICT,
    entry_type        TEXT            NOT NULL,
    amount            NUMERIC(18,6)   NOT NULL CHECK (amount > 0),
    direction         SMALLINT        NOT NULL,    -- +1 credit  |  -1 debit
    balance_before    NUMERIC(18,6)   NOT NULL,
    balance_after     NUMERIC(18,6)   NOT NULL,
    currency          CHAR(3)         NOT NULL,
    payment_reference TEXT,                        -- wire / bank reference
    invoice_number    TEXT,                        -- carrier invoice number
    payment_date      DATE,                        -- value date of payment
    message_id        UUID,                        -- FK to sms_logs for 'charge' entries
    notes             TEXT,
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_carrier_balance_entry_type  CHECK (entry_type IN ('payment','charge','adjustment','refund')),
    CONSTRAINT chk_carrier_balance_direction   CHECK (direction IN (1, -1)),
    CONSTRAINT chk_carrier_balance_amount_pos  CHECK (amount > 0)
);

CREATE INDEX idx_carrier_balance_entries_carrier
    ON carrier_balance_entries (carrier_id, created_at DESC);
CREATE INDEX idx_carrier_balance_entries_type
    ON carrier_balance_entries (entry_type);
CREATE INDEX idx_carrier_balance_entries_message
    ON carrier_balance_entries (message_id)
    WHERE message_id IS NOT NULL;
CREATE INDEX idx_carrier_balance_entries_payment_date
    ON carrier_balance_entries (payment_date DESC)
    WHERE payment_date IS NOT NULL;


-- =============================================================================
-- SECTION 4: ROUTING GROUPS  [NEW in v1.1]
-- =============================================================================
-- A routing group is a named set of prefix-based routing rules.
-- Clients are assigned a routing group; at send time the engine:
--   1. Resolves client.routing_group_id
--   2. Finds the longest-matching active prefix in route_entries
--   3. Tries primary_carrier_id
--   4. On failure, tries failover1_carrier_id (if set)
--   5. On failure, tries failover2_carrier_id (if set)
--   6. Records the winning carrier and failover_sequence in sms_logs
--
-- Routing is independent of billing:
--   Rate   = clients.rate_group_id   (what to charge the client)
--   Route  = clients.routing_group_id (where to send the message)
-- =============================================================================

CREATE TABLE routing_groups (
    routing_group_id    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT        NOT NULL UNIQUE,
    description         TEXT,
    status              TEXT        NOT NULL DEFAULT 'active',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_routing_groups_status CHECK (status IN ('active', 'inactive'))
);

CREATE INDEX idx_routing_groups_status ON routing_groups (status);


-- ---------------------------------------------------------------------------
-- Route entries — prefix-to-carrier mappings within a routing group
-- ---------------------------------------------------------------------------
-- One row per (routing_group_id, prefix) pair.
-- primary_carrier_id   — required; tried first.
-- failover1_carrier_id — optional; tried if primary fails.
-- failover2_carrier_id — optional; tried if failover1 fails.
--                        Cannot be set without failover1 also being set.
-- All three carrier slots must be distinct.
-- priority controls display sort in the Admin UI; lower = shown first.
-- ---------------------------------------------------------------------------

CREATE TABLE route_entries (
    route_entry_id       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    routing_group_id     UUID        NOT NULL REFERENCES routing_groups (routing_group_id) ON DELETE CASCADE,
    prefix               TEXT        NOT NULL,
    description          TEXT,
    priority             INT         NOT NULL DEFAULT 100,
    status               TEXT        NOT NULL DEFAULT 'active',
    primary_carrier_id   UUID        NOT NULL REFERENCES carriers (carrier_id) ON DELETE RESTRICT,
    failover1_carrier_id UUID        REFERENCES carriers (carrier_id) ON DELETE SET NULL,
    failover2_carrier_id UUID        REFERENCES carriers (carrier_id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_route_entries_group_prefix     UNIQUE (routing_group_id, prefix),
    CONSTRAINT chk_route_entries_prefix          CHECK (prefix = '*' OR prefix ~ '^[0-9]+$'),
    CONSTRAINT chk_route_entries_status          CHECK (status IN ('active', 'inactive')),
    CONSTRAINT chk_route_entries_f1_ne_primary   CHECK (failover1_carrier_id IS NULL OR failover1_carrier_id <> primary_carrier_id),
    CONSTRAINT chk_route_entries_f2_ne_primary   CHECK (failover2_carrier_id IS NULL OR failover2_carrier_id <> primary_carrier_id),
    CONSTRAINT chk_route_entries_f2_ne_f1        CHECK (failover2_carrier_id IS NULL OR failover1_carrier_id IS NULL OR failover2_carrier_id <> failover1_carrier_id),
    CONSTRAINT chk_route_entries_failover_order  CHECK (failover2_carrier_id IS NULL OR failover1_carrier_id IS NOT NULL)
);

CREATE INDEX idx_route_entries_group           ON route_entries (routing_group_id);
CREATE INDEX idx_route_entries_prefix          ON route_entries (prefix);
CREATE INDEX idx_route_entries_status          ON route_entries (routing_group_id, status);
CREATE INDEX idx_route_entries_primary_carrier ON route_entries (primary_carrier_id);
CREATE INDEX idx_route_entries_failover1       ON route_entries (failover1_carrier_id) WHERE failover1_carrier_id IS NOT NULL;
CREATE INDEX idx_route_entries_failover2       ON route_entries (failover2_carrier_id) WHERE failover2_carrier_id IS NOT NULL;


-- =============================================================================
-- SECTION 5: CLIENTS
-- =============================================================================
-- routing_group_id [NEW v1.1] — determines which carriers handle outbound SMS.
--   NULL = no routing configured; send requests fail with SMS_ERR_NO_ROUTE.
-- rate_group_id — determines billing rates charged to this client.
-- =============================================================================

CREATE TABLE clients (
    client_id           UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT            NOT NULL,
    email               CITEXT          NOT NULL UNIQUE,
    status              TEXT            NOT NULL DEFAULT 'active',
    rate_group_id       UUID            REFERENCES rate_groups (rate_group_id) ON DELETE SET NULL,
    balance             NUMERIC(18,6)   NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency            CHAR(3)         NOT NULL,
    routing_group_id    UUID            REFERENCES routing_groups (routing_group_id) ON DELETE SET NULL,
    notes               TEXT,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_clients_status   CHECK (status IN ('active', 'suspended', 'disabled')),
    CONSTRAINT chk_clients_currency CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_clients_status         ON clients (status);
CREATE INDEX idx_clients_rate_group     ON clients (rate_group_id);
CREATE INDEX idx_clients_routing_group  ON clients (routing_group_id);


CREATE TABLE client_api_keys (
    key_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES clients (client_id) ON DELETE CASCADE,
    key_hash        TEXT        NOT NULL UNIQUE,
    key_salt        TEXT        NOT NULL,
    key_prefix      CHAR(8)     NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ,
    revoked_reason  TEXT
);

CREATE INDEX idx_client_api_keys_client ON client_api_keys (client_id);
CREATE INDEX idx_client_api_keys_hash   ON client_api_keys (key_hash);
CREATE UNIQUE INDEX uq_client_api_keys_active
    ON client_api_keys (client_id)
    WHERE revoked_at IS NULL;


CREATE TABLE ledger_entries (
    entry_id        UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID            NOT NULL REFERENCES clients (client_id) ON DELETE RESTRICT,
    entry_type      TEXT            NOT NULL,
    amount          NUMERIC(18,6)   NOT NULL CHECK (amount > 0),
    balance_before  NUMERIC(18,6)   NOT NULL,
    balance_after   NUMERIC(18,6)   NOT NULL,
    currency        CHAR(3)         NOT NULL,
    reference       TEXT,
    message_id      UUID,
    notes           TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_ledger_entry_type    CHECK (entry_type IN ('credit', 'debit')),
    CONSTRAINT chk_ledger_balance_after CHECK (balance_after >= 0)
);

CREATE INDEX idx_ledger_entries_client  ON ledger_entries (client_id, created_at DESC);
CREATE INDEX idx_ledger_entries_message ON ledger_entries (message_id);
CREATE INDEX idx_ledger_entries_type    ON ledger_entries (entry_type);


-- =============================================================================
-- SECTION 6: SMS LOGS
-- =============================================================================
-- Routing context [NEW v1.1]:
--   routing_group_id  — routing group used at send time
--   route_entry_id    — specific route entry (prefix match) selected
--   failover_sequence — 0=primary succeeded, 1=failover1, 2=failover2
-- =============================================================================

CREATE TABLE sms_logs (
    message_id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id               UUID            NOT NULL REFERENCES clients (client_id) ON DELETE RESTRICT,
    client_ref              TEXT,
    to_number               TEXT            NOT NULL,
    from_number             TEXT,
    message_body            TEXT            NOT NULL,
    message_length          INT             NOT NULL CHECK (message_length > 0),
    segments                INT             NOT NULL DEFAULT 1 CHECK (segments > 0),
    encoding                TEXT            NOT NULL DEFAULT 'GSM7',
    rate_group_id           UUID            REFERENCES rate_groups (rate_group_id) ON DELETE SET NULL,
    prefix_matched          TEXT,
    rate_applied            NUMERIC(18,6)   NOT NULL CHECK (rate_applied >= 0),
    total_charged           NUMERIC(18,6)   NOT NULL CHECK (total_charged >= 0),
    currency                CHAR(3)         NOT NULL,
    routing_group_id        UUID            REFERENCES routing_groups (routing_group_id) ON DELETE SET NULL,
    route_entry_id          UUID            REFERENCES route_entries (route_entry_id) ON DELETE SET NULL,
    failover_sequence       SMALLINT        NOT NULL DEFAULT 0 CHECK (failover_sequence IN (0, 1, 2)),
    carrier_id              UUID            REFERENCES carriers (carrier_id) ON DELETE SET NULL,
    carrier_message_id      TEXT,
    carrier_response_code   INT,
    carrier_response_body   TEXT,
    status                  TEXT            NOT NULL DEFAULT 'pending',
    received_at             TIMESTAMPTZ     NOT NULL DEFAULT now(),
    dispatched_at           TIMESTAMPTZ,
    delivered_at            TIMESTAMPTZ,
    failed_at               TIMESTAMPTZ,
    CONSTRAINT chk_sms_logs_status   CHECK (status IN ('pending','accepted','sent','delivered','failed','rejected')),
    CONSTRAINT chk_sms_logs_encoding CHECK (encoding IN ('GSM7', 'UCS2'))
);

CREATE INDEX idx_sms_logs_client         ON sms_logs (client_id, received_at DESC);
CREATE INDEX idx_sms_logs_carrier        ON sms_logs (carrier_id, received_at DESC);
CREATE INDEX idx_sms_logs_status         ON sms_logs (status);
CREATE INDEX idx_sms_logs_received       ON sms_logs (received_at DESC);
CREATE INDEX idx_sms_logs_to_number      ON sms_logs (to_number);
CREATE INDEX idx_sms_logs_client_ref     ON sms_logs (client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX idx_sms_logs_routing_group  ON sms_logs (routing_group_id);
CREATE INDEX idx_sms_logs_route_entry    ON sms_logs (route_entry_id);
CREATE INDEX idx_sms_logs_failover       ON sms_logs (failover_sequence) WHERE failover_sequence > 0;


-- =============================================================================
-- SECTION 7: ADMIN AUDIT LOG
-- =============================================================================

CREATE TABLE audit_log (
    audit_id        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        REFERENCES admin_sessions (session_id) ON DELETE SET NULL,
    action          TEXT        NOT NULL,
    entity_type     TEXT        NOT NULL,
    entity_id       UUID,
    entity_name     TEXT,
    payload         JSONB,
    ip_address      INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_action  ON audit_log (action);
CREATE INDEX idx_audit_log_entity  ON audit_log (entity_type, entity_id);
CREATE INDEX idx_audit_log_created ON audit_log (created_at DESC);
CREATE INDEX idx_audit_log_session ON audit_log (session_id);


-- =============================================================================
-- SECTION 8: SYSTEM SETTINGS
-- =============================================================================

CREATE TABLE system_settings (
    key         TEXT    PRIMARY KEY,
    value       TEXT    NOT NULL,
    description TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO system_settings (key, value, description) VALUES
    ('default_sender_id',           'MiniSMS', 'Default originator when client omits from field'),
    ('carrier_dispatch_timeout_s',  '10',      'Per-carrier HTTP dispatch timeout in seconds'),
    ('low_balance_alert_threshold', '1.00',    'Alert when any client balance drops below this'),
    ('refund_on_carrier_failure',   'true',    'Refund client on carrier network-level error'),
    ('max_sms_segments',            '4',       'Maximum concatenated segments per send request'),
    ('admin_session_idle_minutes',  '240',     'Admin UI session idle timeout in minutes'),
    ('api_rate_limit_per_minute',   '60',      'Default API requests/minute per client'),
    ('failover_enabled',            'true',    'Enable automatic carrier failover'),
    ('carrier_low_balance_alert',   '10.00',   'Alert when any carrier balance drops below this')
ON CONFLICT (key) DO NOTHING;


-- =============================================================================
-- SECTION 9: FUNCTIONS AND TRIGGERS
-- =============================================================================

-- ---------------------------------------------------------------------------
-- updated_at trigger function
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_rate_groups_updated_at
    BEFORE UPDATE ON rate_groups FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_rate_entries_updated_at
    BEFORE UPDATE ON rate_entries FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_carriers_updated_at
    BEFORE UPDATE ON carriers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_carrier_auth_headers_updated_at
    BEFORE UPDATE ON carrier_auth_headers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_carrier_request_templates_updated_at
    BEFORE UPDATE ON carrier_request_templates FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_routing_groups_updated_at
    BEFORE UPDATE ON routing_groups FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_route_entries_updated_at
    BEFORE UPDATE ON route_entries FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_clients_updated_at
    BEFORE UPDATE ON clients FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_system_settings_updated_at
    BEFORE UPDATE ON system_settings FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ---------------------------------------------------------------------------
-- Immutability guards
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION deny_ledger_update()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries rows are immutable; UPDATE is not permitted';
END;
$$;

CREATE TRIGGER trg_ledger_entries_no_update
    BEFORE UPDATE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION deny_ledger_update();


CREATE OR REPLACE FUNCTION deny_carrier_balance_update()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'carrier_balance_entries rows are immutable; UPDATE is not permitted';
END;
$$;

CREATE TRIGGER trg_carrier_balance_entries_no_update
    BEFORE UPDATE ON carrier_balance_entries FOR EACH ROW EXECUTE FUNCTION deny_carrier_balance_update();


CREATE OR REPLACE FUNCTION deny_audit_update()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log rows are immutable; UPDATE is not permitted';
END;
$$;

CREATE TRIGGER trg_audit_log_no_update
    BEFORE UPDATE ON audit_log FOR EACH ROW EXECUTE FUNCTION deny_audit_update();


-- ---------------------------------------------------------------------------
-- record_carrier_payment()  [NEW v1.1]
-- ---------------------------------------------------------------------------
-- Records a payment from the operator to the carrier (prepay top-up).
-- Atomically locks the carrier row, writes a ledger entry, updates the balance.
-- Returns the new carrier balance.
--
-- Example:
--   SELECT record_carrier_payment(
--       p_carrier_id        => '<uuid>',
--       p_amount            => 500.00,
--       p_currency          => 'GBP',
--       p_payment_reference => 'WIRE-20260420-001',
--       p_invoice_number    => 'INV-98765',
--       p_payment_date      => '2026-04-20',
--       p_notes             => 'Q2 prepayment'
--   );
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION record_carrier_payment(
    p_carrier_id        UUID,
    p_amount            NUMERIC(18,6),
    p_currency          CHAR(3),
    p_payment_reference TEXT    DEFAULT NULL,
    p_invoice_number    TEXT    DEFAULT NULL,
    p_payment_date      DATE    DEFAULT CURRENT_DATE,
    p_notes             TEXT    DEFAULT NULL
)
RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE
    v_balance_before  NUMERIC(18,6);
    v_balance_after   NUMERIC(18,6);
BEGIN
    IF p_amount <= 0 THEN
        RAISE EXCEPTION 'Payment amount must be positive; got %', p_amount
            USING ERRCODE = 'P0002';
    END IF;

    SELECT balance INTO v_balance_before
    FROM   carriers
    WHERE  carrier_id = p_carrier_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'Carrier % not found', p_carrier_id
            USING ERRCODE = 'P0003';
    END IF;

    v_balance_after := v_balance_before + p_amount;

    UPDATE carriers
    SET    balance    = v_balance_after,
           updated_at = now()
    WHERE  carrier_id = p_carrier_id;

    INSERT INTO carrier_balance_entries (
        carrier_id,
        entry_type, amount, direction,
        balance_before, balance_after, currency,
        payment_reference, invoice_number, payment_date,
        notes
    ) VALUES (
        p_carrier_id,
        'payment', p_amount, 1,
        v_balance_before, v_balance_after, p_currency,
        p_payment_reference, p_invoice_number, p_payment_date,
        p_notes
    );

    RETURN v_balance_after;
END;
$$;


-- ---------------------------------------------------------------------------
-- deduct_carrier_balance()  [NEW v1.1]
-- ---------------------------------------------------------------------------
-- Posts a per-SMS charge against the carrier account after successful dispatch.
-- Does not raise on negative balance (carrier may allow invoice/overdraft).
-- Returns the new carrier balance.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION deduct_carrier_balance(
    p_carrier_id    UUID,
    p_amount        NUMERIC(18,6),
    p_currency      CHAR(3),
    p_message_id    UUID
)
RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE
    v_balance_before  NUMERIC(18,6);
    v_balance_after   NUMERIC(18,6);
BEGIN
    SELECT balance INTO v_balance_before
    FROM   carriers
    WHERE  carrier_id = p_carrier_id
    FOR UPDATE;

    v_balance_after := v_balance_before - p_amount;

    UPDATE carriers
    SET    balance    = v_balance_after,
           updated_at = now()
    WHERE  carrier_id = p_carrier_id;

    INSERT INTO carrier_balance_entries (
        carrier_id,
        entry_type, amount, direction,
        balance_before, balance_after,
        currency, message_id
    ) VALUES (
        p_carrier_id,
        'charge', p_amount, -1,
        v_balance_before, v_balance_after,
        p_currency, p_message_id
    );

    RETURN v_balance_after;
END;
$$;


-- ---------------------------------------------------------------------------
-- increment_carrier_usage()
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION increment_carrier_usage(
    p_carrier_id  UUID,
    p_segments    INT,
    p_amount      NUMERIC(18,6)
)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO carrier_usage_totals (
        carrier_id, total_messages, total_segments, total_amount, last_message_at
    ) VALUES (p_carrier_id, 1, p_segments, p_amount, now())
    ON CONFLICT (carrier_id) DO UPDATE SET
        total_messages  = carrier_usage_totals.total_messages + 1,
        total_segments  = carrier_usage_totals.total_segments + EXCLUDED.total_segments,
        total_amount    = carrier_usage_totals.total_amount   + EXCLUDED.total_amount,
        last_message_at = now(),
        updated_at      = now();
END;
$$;


-- ---------------------------------------------------------------------------
-- deduct_client_balance()
-- ---------------------------------------------------------------------------
-- Atomically checks balance, deducts, writes ledger entry.
-- Raises SQLSTATE P0001 if balance < p_amount.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION deduct_client_balance(
    p_client_id   UUID,
    p_amount      NUMERIC(18,6),
    p_message_id  UUID,
    p_currency    CHAR(3)
)
RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE
    v_balance_before  NUMERIC(18,6);
    v_balance_after   NUMERIC(18,6);
BEGIN
    SELECT balance INTO v_balance_before
    FROM   clients
    WHERE  client_id = p_client_id
    FOR UPDATE;

    IF v_balance_before < p_amount THEN
        RAISE EXCEPTION 'INSUFFICIENT_BALANCE: client % has % but requires %',
            p_client_id, v_balance_before, p_amount
            USING ERRCODE = 'P0001';
    END IF;

    v_balance_after := v_balance_before - p_amount;

    UPDATE clients
    SET    balance    = v_balance_after,
           updated_at = now()
    WHERE  client_id = p_client_id;

    INSERT INTO ledger_entries (
        client_id, entry_type, amount,
        balance_before, balance_after, currency,
        reference, message_id
    ) VALUES (
        p_client_id, 'debit', p_amount,
        v_balance_before, v_balance_after, p_currency,
        p_message_id::TEXT, p_message_id
    );

    RETURN v_balance_after;
END;
$$;


-- ---------------------------------------------------------------------------
-- credit_client_balance()
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION credit_client_balance(
    p_client_id   UUID,
    p_amount      NUMERIC(18,6),
    p_currency    CHAR(3),
    p_reference   TEXT  DEFAULT NULL,
    p_notes       TEXT  DEFAULT NULL
)
RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE
    v_balance_before  NUMERIC(18,6);
    v_balance_after   NUMERIC(18,6);
BEGIN
    SELECT balance INTO v_balance_before
    FROM   clients
    WHERE  client_id = p_client_id
    FOR UPDATE;

    v_balance_after := v_balance_before + p_amount;

    UPDATE clients
    SET    balance    = v_balance_after,
           updated_at = now()
    WHERE  client_id = p_client_id;

    INSERT INTO ledger_entries (
        client_id, entry_type, amount,
        balance_before, balance_after, currency,
        reference, notes
    ) VALUES (
        p_client_id, 'credit', p_amount,
        v_balance_before, v_balance_after, p_currency,
        p_reference, p_notes
    );

    RETURN v_balance_after;
END;
$$;


-- =============================================================================
-- SECTION 10: VIEWS
-- =============================================================================

CREATE OR REPLACE VIEW v_active_rate_entries AS
SELECT
    re.rate_entry_id,
    re.rate_group_id,
    rg.name         AS rate_group_name,
    rg.currency,
    re.prefix,
    re.description,
    re.rate_per_sms,
    re.effective_from,
    re.effective_to
FROM rate_entries re
JOIN rate_groups  rg ON rg.rate_group_id = re.rate_group_id
WHERE CURRENT_DATE >= re.effective_from
  AND (re.effective_to IS NULL OR CURRENT_DATE <= re.effective_to);


CREATE OR REPLACE VIEW v_active_api_keys AS
SELECT
    k.key_id,
    k.client_id,
    k.key_hash,
    k.key_salt,
    k.key_prefix,
    k.created_at,
    c.name              AS client_name,
    c.status            AS client_status,
    c.balance,
    c.currency,
    c.rate_group_id,
    c.routing_group_id
FROM client_api_keys k
JOIN clients         c ON c.client_id = k.client_id
WHERE k.revoked_at IS NULL;


-- Routing group summary with route counts  [NEW v1.1]
CREATE OR REPLACE VIEW v_routing_group_detail AS
SELECT
    rg.routing_group_id,
    rg.name,
    rg.description,
    rg.status,
    rg.created_at,
    rg.updated_at,
    COUNT(re.route_entry_id)                                             AS total_routes,
    COUNT(re.route_entry_id) FILTER (WHERE re.status = 'active')         AS active_routes,
    COUNT(re.route_entry_id) FILTER (WHERE re.failover1_carrier_id IS NOT NULL) AS routes_with_failover1,
    COUNT(re.route_entry_id) FILTER (WHERE re.failover2_carrier_id IS NOT NULL) AS routes_with_failover2
FROM routing_groups rg
LEFT JOIN route_entries re ON re.routing_group_id = rg.routing_group_id
GROUP BY rg.routing_group_id;


-- Full route entry detail with all carrier names  [NEW v1.1]
CREATE OR REPLACE VIEW v_route_entries_detail AS
SELECT
    re.route_entry_id,
    re.routing_group_id,
    rg.name                         AS routing_group_name,
    re.prefix,
    re.description,
    re.priority,
    re.status,
    re.primary_carrier_id,
    cp.name                         AS primary_carrier_name,
    cp.status                       AS primary_carrier_status,
    cp.balance                      AS primary_carrier_balance,
    re.failover1_carrier_id,
    cf1.name                        AS failover1_carrier_name,
    cf1.status                      AS failover1_carrier_status,
    cf1.balance                     AS failover1_carrier_balance,
    re.failover2_carrier_id,
    cf2.name                        AS failover2_carrier_name,
    cf2.status                      AS failover2_carrier_status,
    cf2.balance                     AS failover2_carrier_balance,
    re.created_at,
    re.updated_at
FROM route_entries  re
JOIN routing_groups rg  ON  rg.routing_group_id  = re.routing_group_id
JOIN carriers       cp  ON  cp.carrier_id         = re.primary_carrier_id
LEFT JOIN carriers  cf1 ON  cf1.carrier_id        = re.failover1_carrier_id
LEFT JOIN carriers  cf2 ON  cf2.carrier_id        = re.failover2_carrier_id;


-- Carrier financial position  [NEW v1.1]
CREATE OR REPLACE VIEW v_carrier_financial_position AS
SELECT
    c.carrier_id,
    c.name                                      AS carrier_name,
    c.status,
    c.balance                                   AS current_balance,
    c.currency,
    COALESCE(cu.total_messages, 0)              AS total_messages_sent,
    COALESCE(cu.total_amount,   0)              AS total_amount_charged_all_time,
    cu.last_message_at,
    COALESCE(SUM(cbe.amount)
        FILTER (WHERE cbe.entry_type = 'charge'
                  AND cbe.created_at >= now() - INTERVAL '30 days'), 0)
                                                AS spend_last_30d,
    COALESCE(SUM(cbe.amount)
        FILTER (WHERE cbe.entry_type = 'payment'), 0)
                                                AS total_payments_received,
    COALESCE(SUM(cbe.amount)
        FILTER (WHERE cbe.entry_type = 'refund'), 0)
                                                AS total_refunds_received,
    c.updated_at
FROM carriers                   c
LEFT JOIN carrier_usage_totals    cu  ON cu.carrier_id  = c.carrier_id
LEFT JOIN carrier_balance_entries cbe ON cbe.carrier_id = c.carrier_id
GROUP BY c.carrier_id, c.name, c.status, c.balance, c.currency,
         cu.total_messages, cu.total_amount, cu.last_message_at, c.updated_at;


-- Client SMS summary — last 30 days  [UPDATED v1.1]
CREATE OR REPLACE VIEW v_client_sms_summary AS
SELECT
    sl.client_id,
    c.name                                                  AS client_name,
    c.routing_group_id,
    rg.name                                                 AS routing_group_name,
    COUNT(*)                                                AS total_messages,
    SUM(sl.segments)                                        AS total_segments,
    SUM(sl.total_charged)                                   AS total_charged,
    COUNT(*) FILTER (WHERE sl.status = 'delivered')         AS delivered_count,
    COUNT(*) FILTER (WHERE sl.status = 'failed')            AS failed_count,
    COUNT(*) FILTER (WHERE sl.status = 'rejected')          AS rejected_count,
    COUNT(*) FILTER (WHERE sl.failover_sequence = 1)        AS failover1_used_count,
    COUNT(*) FILTER (WHERE sl.failover_sequence = 2)        AS failover2_used_count,
    MAX(sl.received_at)                                     AS last_message_at
FROM sms_logs            sl
JOIN clients             c   ON  c.client_id          = sl.client_id
LEFT JOIN routing_groups rg  ON  rg.routing_group_id  = c.routing_group_id
WHERE sl.received_at >= now() - INTERVAL '30 days'
GROUP BY sl.client_id, c.name, c.routing_group_id, rg.name;


-- Carrier dispatch summary — last 30 days  [UPDATED v1.1]
CREATE OR REPLACE VIEW v_carrier_sms_summary AS
SELECT
    sl.carrier_id,
    c.name                                                          AS carrier_name,
    COUNT(*)                                                        AS total_messages,
    SUM(sl.segments)                                                AS total_segments,
    COUNT(*) FILTER (WHERE sl.failover_sequence = 0)                AS dispatched_as_primary,
    COUNT(*) FILTER (WHERE sl.failover_sequence = 1)                AS dispatched_as_failover1,
    COUNT(*) FILTER (WHERE sl.failover_sequence = 2)                AS dispatched_as_failover2,
    COUNT(*) FILTER (WHERE sl.status = 'failed')                    AS failed_count,
    ROUND(
        COUNT(*) FILTER (WHERE sl.status IN ('sent', 'delivered'))::NUMERIC
        / NULLIF(COUNT(*), 0) * 100, 2
    )                                                               AS success_rate_pct,
    MAX(sl.dispatched_at)                                           AS last_dispatched_at
FROM sms_logs  sl
JOIN carriers  c ON c.carrier_id = sl.carrier_id
WHERE sl.received_at >= now() - INTERVAL '30 days'
  AND sl.carrier_id  IS NOT NULL
GROUP BY sl.carrier_id, c.name;


-- =============================================================================
-- SECTION 11: DATABASE ROLES AND PERMISSIONS
-- =============================================================================
-- Run as superuser. Replace 'CHANGE_ME' with a secret from your vault.
-- =============================================================================

-- CREATE ROLE minisms_app LOGIN PASSWORD 'CHANGE_ME';

-- GRANT SELECT, INSERT, UPDATE ON
--     admin_sessions, rate_groups, rate_entries,
--     routing_groups, route_entries,
--     carriers, carrier_auth_headers, carrier_request_templates,
--     carrier_usage_totals, clients, client_api_keys,
--     sms_logs, system_settings
-- TO minisms_app;

-- GRANT SELECT, INSERT ON
--     ledger_entries, carrier_balance_entries, audit_log
-- TO minisms_app;

-- GRANT EXECUTE ON FUNCTION
--     record_carrier_payment(UUID, NUMERIC, CHAR, TEXT, TEXT, DATE, TEXT),
--     deduct_carrier_balance(UUID, NUMERIC, CHAR, UUID),
--     increment_carrier_usage(UUID, INT, NUMERIC),
--     deduct_client_balance(UUID, NUMERIC, UUID, CHAR),
--     credit_client_balance(UUID, NUMERIC, CHAR, TEXT, TEXT)
-- TO minisms_app;

-- GRANT SELECT ON
--     v_active_rate_entries, v_active_api_keys,
--     v_routing_group_detail, v_route_entries_detail,
--     v_carrier_financial_position,
--     v_client_sms_summary, v_carrier_sms_summary
-- TO minisms_app;


-- =============================================================================
-- END OF SCHEMA  (v1.1)
-- =============================================================================

-- >>> 002_v1.2_currencies_senderids.up.sql <<<
-- 1. currencies table
CREATE TABLE IF NOT EXISTS currencies (
    code            CHAR(3)     PRIMARY KEY,
    name            TEXT        NOT NULL,
    symbol          VARCHAR(8)  NOT NULL DEFAULT '',
    decimal_places  SMALLINT    NOT NULL DEFAULT 2 CHECK (decimal_places BETWEEN 0 AND 6),
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_currencies_code CHECK (code ~ '^[A-Z]{3}$')
);
CREATE INDEX IF NOT EXISTS idx_currencies_active ON currencies (is_active);

-- 2. Seed 20 currencies (INSERT ... ON CONFLICT DO NOTHING)
INSERT INTO currencies (code, name, symbol, decimal_places) VALUES
  ('GBP','British Pound Sterling','£',2), ('USD','United States Dollar','$',2),
  ('EUR','Euro','€',2), ('AED','UAE Dirham','د.إ',2), ('SAR','Saudi Riyal','﷼',2),
  ('PKR','Pakistani Rupee','₨',2), ('INR','Indian Rupee','₹',2),
  ('BDT','Bangladeshi Taka','৳',2), ('NGN','Nigerian Naira','₦',2),
  ('ZAR','South African Rand','R',2), ('TRY','Turkish Lira','₺',2),
  ('CHF','Swiss Franc','Fr',2), ('CAD','Canadian Dollar','C$',2),
  ('AUD','Australian Dollar','A$',2), ('JPY','Japanese Yen','¥',0),
  ('CNY','Chinese Yuan Renminbi','¥',2), ('SEK','Swedish Krona','kr',2),
  ('NOK','Norwegian Krone','kr',2), ('DKK','Danish Krone','kr',2),
  ('SGD','Singapore Dollar','S$',2)
ON CONFLICT (code) DO NOTHING;

-- 3. Add FK from rate_groups, carriers, clients to currencies
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_rate_groups_currency') THEN
    ALTER TABLE rate_groups ADD CONSTRAINT fk_rate_groups_currency
      FOREIGN KEY (currency) REFERENCES currencies(code) ON UPDATE CASCADE;
  END IF;
END $$;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_carriers_currency') THEN
    ALTER TABLE carriers ADD CONSTRAINT fk_carriers_currency
      FOREIGN KEY (currency) REFERENCES currencies(code) ON UPDATE CASCADE;
  END IF;
END $$;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_clients_currency') THEN
    ALTER TABLE clients ADD CONSTRAINT fk_clients_currency
      FOREIGN KEY (currency) REFERENCES currencies(code) ON UPDATE CASCADE;
  END IF;
END $$;

-- 4. Add new carrier columns
ALTER TABLE carriers
  ADD COLUMN IF NOT EXISTS sender_id_policy        TEXT NOT NULL DEFAULT 'any',
  ADD COLUMN IF NOT EXISTS default_sender_id_value TEXT;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_carriers_sender_policy') THEN
    ALTER TABLE carriers ADD CONSTRAINT chk_carriers_sender_policy
      CHECK (sender_id_policy IN ('any','numeric','e164','list','none'));
  END IF;
END $$;

-- 5. Add new client columns
ALTER TABLE clients
  ADD COLUMN IF NOT EXISTS default_sender_id_value TEXT,
  ADD COLUMN IF NOT EXISTS allow_any_sender_id     BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS allow_in_loss_delivery  BOOLEAN NOT NULL DEFAULT TRUE;

-- 6. sender_ids table
CREATE TABLE IF NOT EXISTS sender_ids (
    sender_id       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    value           TEXT        NOT NULL UNIQUE,
    sender_id_type  TEXT        NOT NULL DEFAULT 'alpha',
    description     TEXT,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_sender_ids_type CHECK (sender_id_type IN ('alpha','numeric','e164')),
    CONSTRAINT chk_sender_ids_value_len CHECK (char_length(value) BETWEEN 1 AND 15)
);
CREATE INDEX IF NOT EXISTS idx_sender_ids_value  ON sender_ids (value);
CREATE INDEX IF NOT EXISTS idx_sender_ids_active ON sender_ids (is_active);

-- 7. client_sender_ids
CREATE TABLE IF NOT EXISTS client_sender_ids (
    client_sender_id UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id        UUID        NOT NULL REFERENCES clients(client_id) ON DELETE CASCADE,
    sender_id        UUID        NOT NULL REFERENCES sender_ids(sender_id) ON DELETE RESTRICT,
    is_default       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_client_sender_ids UNIQUE (client_id, sender_id)
);
CREATE INDEX IF NOT EXISTS idx_client_sender_ids_client ON client_sender_ids (client_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_client_sender_ids_default ON client_sender_ids(client_id) WHERE is_default = TRUE;

-- 8. carrier_sender_ids
CREATE TABLE IF NOT EXISTS carrier_sender_ids (
    carrier_sender_id UUID       PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id        UUID       NOT NULL REFERENCES carriers(carrier_id) ON DELETE CASCADE,
    sender_id         UUID       NOT NULL REFERENCES sender_ids(sender_id) ON DELETE RESTRICT,
    is_default        BOOLEAN    NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_carrier_sender_ids UNIQUE (carrier_id, sender_id)
);
CREATE INDEX IF NOT EXISTS idx_carrier_sender_ids_carrier ON carrier_sender_ids (carrier_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_carrier_sender_ids_default ON carrier_sender_ids(carrier_id) WHERE is_default = TRUE;

-- 9. New sms_logs columns
ALTER TABLE sms_logs
  ADD COLUMN IF NOT EXISTS sender_id_source    TEXT NOT NULL DEFAULT 'client_provided',
  ADD COLUMN IF NOT EXISTS carrier_skip_reason JSONB;
CREATE INDEX IF NOT EXISTS idx_sms_logs_sid_source ON sms_logs(sender_id_source);

-- 10. Add updated_at triggers for new tables
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END; $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_currencies_updated_at') THEN
    CREATE TRIGGER trg_currencies_updated_at BEFORE UPDATE ON currencies FOR EACH ROW EXECUTE FUNCTION set_updated_at();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_sender_ids_updated_at') THEN
    CREATE TRIGGER trg_sender_ids_updated_at BEFORE UPDATE ON sender_ids FOR EACH ROW EXECUTE FUNCTION set_updated_at();
  END IF;
END $$;

-- 11. New and updated views (CREATE OR REPLACE is safe — idempotent)
CREATE OR REPLACE VIEW v_client_sender_ids AS
SELECT csi.client_sender_id, csi.client_id, c.name AS client_name,
       csi.sender_id, si.value AS sender_id_value, si.sender_id_type,
       csi.is_default, csi.created_at
FROM client_sender_ids csi
JOIN clients c ON c.client_id = csi.client_id
JOIN sender_ids si ON si.sender_id = csi.sender_id
WHERE si.is_active = TRUE;

CREATE OR REPLACE VIEW v_carrier_sender_ids AS
SELECT csid.carrier_sender_id, csid.carrier_id, ca.name AS carrier_name,
       ca.sender_id_policy, ca.default_sender_id_value,
       csid.sender_id, si.value AS sender_id_value, si.sender_id_type,
       csid.is_default, csid.created_at
FROM carrier_sender_ids csid
JOIN carriers ca ON ca.carrier_id = csid.carrier_id
JOIN sender_ids si ON si.sender_id = csid.sender_id
WHERE si.is_active = TRUE;

CREATE OR REPLACE VIEW v_carrier_prefix_success AS
SELECT sl.carrier_id, c.name AS carrier_name, sl.prefix_matched,
  COUNT(*) AS total_messages,
  COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered')) AS success_count,
  COUNT(*) FILTER (WHERE sl.status = 'failed') AS failed_count,
  ROUND(COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::NUMERIC / NULLIF(COUNT(*),0) * 100, 2) AS success_rate_pct
FROM sms_logs sl JOIN carriers c ON c.carrier_id = sl.carrier_id
WHERE sl.carrier_id IS NOT NULL AND sl.prefix_matched IS NOT NULL
GROUP BY sl.carrier_id, c.name, sl.prefix_matched;

CREATE OR REPLACE VIEW v_client_bill_vs_carrier_cost AS
SELECT sl.client_id, cl.name AS client_name, sl.carrier_id, ca.name AS carrier_name,
  COUNT(*) AS total_messages,
  SUM(sl.total_charged) AS client_billed_total,
  COALESCE(SUM(cbe.amount),0) AS carrier_cost_total,
  SUM(sl.total_charged) - COALESCE(SUM(cbe.amount),0) AS margin,
  ROUND((SUM(sl.total_charged) - COALESCE(SUM(cbe.amount),0)) / NULLIF(SUM(sl.total_charged),0) * 100, 2) AS margin_pct
FROM sms_logs sl
JOIN clients cl ON cl.client_id = sl.client_id
JOIN carriers ca ON ca.carrier_id = sl.carrier_id
LEFT JOIN carrier_balance_entries cbe ON cbe.message_id = sl.message_id AND cbe.entry_type = 'charge'
WHERE sl.status IN ('accepted','sent','delivered') AND sl.carrier_id IS NOT NULL
GROUP BY sl.client_id, cl.name, sl.carrier_id, ca.name;

-- End of migration 002

-- >>> 003_dlr_support.up.sql <<<
ALTER TABLE clients ADD COLUMN IF NOT EXISTS dlr_webhook_url TEXT DEFAULT NULL;
ALTER TABLE clients ADD COLUMN IF NOT EXISTS dlr_webhook_secret TEXT DEFAULT NULL;

ALTER TABLE carriers ADD COLUMN IF NOT EXISTS dlr_callback_url_template TEXT DEFAULT NULL;
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS dlr_field_name TEXT DEFAULT NULL;
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS dlr_inbound_secret TEXT DEFAULT NULL;
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS dlr_message_id_field TEXT DEFAULT NULL;
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS dlr_status_field TEXT DEFAULT NULL;
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS dlr_status_map JSONB DEFAULT NULL;
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS smpp_source_addr_ton TEXT NOT NULL DEFAULT 'dynamic';
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS smpp_source_addr_npi TEXT NOT NULL DEFAULT 'dynamic';
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS smpp_dest_addr_ton TEXT NOT NULL DEFAULT 'dynamic';
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS smpp_dest_addr_npi TEXT NOT NULL DEFAULT 'dynamic';

ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_requested BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_webhook_url TEXT DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_status TEXT DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_received_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_forwarded_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_forward_status TEXT DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dlr_forward_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS source_addr_ton SMALLINT DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS source_addr_npi SMALLINT DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dest_addr_ton SMALLINT DEFAULT NULL;
ALTER TABLE sms_logs ADD COLUMN IF NOT EXISTS dest_addr_npi SMALLINT DEFAULT NULL;

CREATE INDEX IF NOT EXISTS idx_sms_logs_dlr_requested
    ON sms_logs (dlr_requested) WHERE dlr_requested = TRUE;
CREATE INDEX IF NOT EXISTS idx_sms_logs_dlr_status
    ON sms_logs (dlr_status) WHERE dlr_status IS NOT NULL;

-- >>> 004_smpp.up.sql <<<
-- SMPP v3.4 interconnect schema (ADR-001). Defaults preserve HTTP-only behavior for existing rows.

-- =============================================================================
-- carriers: egress transport + outbound SMPP (ESME client) settings
-- =============================================================================

ALTER TABLE carriers
    ADD COLUMN IF NOT EXISTS egress_transport TEXT NOT NULL DEFAULT 'http',
    ADD COLUMN IF NOT EXISTS smpp_host TEXT,
    ADD COLUMN IF NOT EXISTS smpp_port INT,
    ADD COLUMN IF NOT EXISTS smpp_system_id TEXT,
    ADD COLUMN IF NOT EXISTS smpp_password_enc TEXT,
    ADD COLUMN IF NOT EXISTS smpp_system_type TEXT,
    ADD COLUMN IF NOT EXISTS smpp_bind_mode TEXT NOT NULL DEFAULT 'trx',
    ADD COLUMN IF NOT EXISTS smpp_interface_version SMALLINT NOT NULL DEFAULT 52,
    ADD COLUMN IF NOT EXISTS smpp_tls BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS smpp_enquire_link_s INT NOT NULL DEFAULT 30,
    ADD COLUMN IF NOT EXISTS smpp_window_size INT NOT NULL DEFAULT 10,
    ADD COLUMN IF NOT EXISTS smpp_throughput_per_s INT NOT NULL DEFAULT 50,
    ADD COLUMN IF NOT EXISTS smpp_status TEXT NOT NULL DEFAULT 'disabled';

ALTER TABLE carriers
    DROP CONSTRAINT IF EXISTS chk_carriers_egress_transport,
    ADD CONSTRAINT chk_carriers_egress_transport
        CHECK (egress_transport IN ('http', 'smpp', 'both')),
    DROP CONSTRAINT IF EXISTS chk_carriers_smpp_bind_mode,
    ADD CONSTRAINT chk_carriers_smpp_bind_mode
        CHECK (smpp_bind_mode IN ('tx', 'rx', 'trx')),
    DROP CONSTRAINT IF EXISTS chk_carriers_smpp_port,
    ADD CONSTRAINT chk_carriers_smpp_port
        CHECK (smpp_port IS NULL OR (smpp_port >= 1 AND smpp_port <= 65535)),
    DROP CONSTRAINT IF EXISTS chk_carriers_smpp_enquire_link_s,
    ADD CONSTRAINT chk_carriers_smpp_enquire_link_s
        CHECK (smpp_enquire_link_s >= 5 AND smpp_enquire_link_s <= 3600),
    DROP CONSTRAINT IF EXISTS chk_carriers_smpp_window_size,
    ADD CONSTRAINT chk_carriers_smpp_window_size
        CHECK (smpp_window_size >= 1 AND smpp_window_size <= 1000),
    DROP CONSTRAINT IF EXISTS chk_carriers_smpp_throughput_per_s,
    ADD CONSTRAINT chk_carriers_smpp_throughput_per_s
        CHECK (smpp_throughput_per_s >= 1 AND smpp_throughput_per_s <= 10000),
    DROP CONSTRAINT IF EXISTS chk_carriers_smpp_status,
    ADD CONSTRAINT chk_carriers_smpp_status
        CHECK (smpp_status IN ('disabled', 'down', 'binding', 'up', 'throttled'));

CREATE UNIQUE INDEX IF NOT EXISTS uq_carriers_smpp_system_id
    ON carriers (smpp_system_id)
    WHERE smpp_system_id IS NOT NULL AND trim(smpp_system_id) <> '';

-- =============================================================================
-- clients: ingress SMPP (ESME binds to MiniSMS SMSC) + DLR delivery preference
-- =============================================================================

ALTER TABLE clients
    ADD COLUMN IF NOT EXISTS smpp_ingress_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS smpp_system_id TEXT,
    ADD COLUMN IF NOT EXISTS smpp_password_enc TEXT,
    ADD COLUMN IF NOT EXISTS smpp_allowed_cidrs TEXT,
    ADD COLUMN IF NOT EXISTS smpp_max_binds INT NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS smpp_default_src_ton SMALLINT,
    ADD COLUMN IF NOT EXISTS smpp_default_src_npi SMALLINT,
    ADD COLUMN IF NOT EXISTS smpp_throughput_per_s INT NOT NULL DEFAULT 50,
    ADD COLUMN IF NOT EXISTS dlr_delivery_mode TEXT NOT NULL DEFAULT 'http';

ALTER TABLE clients
    DROP CONSTRAINT IF EXISTS chk_clients_smpp_max_binds,
    ADD CONSTRAINT chk_clients_smpp_max_binds
        CHECK (smpp_max_binds >= 0 AND smpp_max_binds <= 100),
    DROP CONSTRAINT IF EXISTS chk_clients_smpp_throughput_per_s,
    ADD CONSTRAINT chk_clients_smpp_throughput_per_s
        CHECK (smpp_throughput_per_s >= 1 AND smpp_throughput_per_s <= 10000),
    DROP CONSTRAINT IF EXISTS chk_clients_dlr_delivery_mode,
    ADD CONSTRAINT chk_clients_dlr_delivery_mode
        CHECK (dlr_delivery_mode IN ('http', 'smpp', 'both'));

CREATE UNIQUE INDEX IF NOT EXISTS uq_clients_smpp_system_id
    ON clients (smpp_system_id)
    WHERE smpp_system_id IS NOT NULL AND trim(smpp_system_id) <> '';

-- =============================================================================
-- sms_logs: transport audit (carrier_message_id already exists)
-- =============================================================================

ALTER TABLE sms_logs
    ADD COLUMN IF NOT EXISTS ingress_transport TEXT NOT NULL DEFAULT 'http',
    ADD COLUMN IF NOT EXISTS egress_transport TEXT;

ALTER TABLE sms_logs
    DROP CONSTRAINT IF EXISTS chk_sms_logs_ingress_transport,
    ADD CONSTRAINT chk_sms_logs_ingress_transport
        CHECK (ingress_transport IN ('http', 'smpp')),
    DROP CONSTRAINT IF EXISTS chk_sms_logs_egress_transport,
    ADD CONSTRAINT chk_sms_logs_egress_transport
        CHECK (egress_transport IS NULL OR egress_transport IN ('http', 'smpp'));

CREATE INDEX IF NOT EXISTS idx_sms_logs_ingress_transport
    ON sms_logs (ingress_transport);
CREATE INDEX IF NOT EXISTS idx_sms_logs_egress_transport
    ON sms_logs (egress_transport)
    WHERE egress_transport IS NOT NULL;

-- =============================================================================
-- smpp_bind_events: non-financial bind/session audit (optional observability)
-- =============================================================================

CREATE TABLE IF NOT EXISTS smpp_bind_events (
    event_id        UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type     TEXT            NOT NULL,
    entity_id       UUID            NOT NULL,
    remote_addr     INET,
    bind_mode       TEXT,
    event_type      TEXT            NOT NULL,
    command_status  INT,
    detail          TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_smpp_bind_events_entity_type CHECK (entity_type IN ('client', 'carrier')),
    CONSTRAINT chk_smpp_bind_events_bind_mode CHECK (bind_mode IS NULL OR bind_mode IN ('tx', 'rx', 'trx')),
    CONSTRAINT chk_smpp_bind_events_event_type CHECK (event_type IN (
        'bind_attempt', 'bind_ok', 'bind_fail', 'unbind', 'disconnect', 'enquire_link_timeout'
    ))
);

CREATE INDEX IF NOT EXISTS idx_smpp_bind_events_entity
    ON smpp_bind_events (entity_type, entity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_smpp_bind_events_created
    ON smpp_bind_events (created_at DESC);

-- >>> 005_carrier_interconnect.up.sql <<<
-- Carrier interconnect: HTTP or SMPP only (remove "both").

UPDATE carriers SET egress_transport = 'smpp'
WHERE egress_transport = 'both'
  AND smpp_host IS NOT NULL AND trim(smpp_host) <> '';

UPDATE carriers SET egress_transport = 'http'
WHERE egress_transport = 'both';

ALTER TABLE carriers DROP CONSTRAINT IF EXISTS chk_carriers_egress_transport;
ALTER TABLE carriers ADD CONSTRAINT chk_carriers_egress_transport
    CHECK (egress_transport IN ('http', 'smpp'));

-- >>> 006_client_allowed_sender_ids.up.sql <<<
-- Client allowed sender IDs mode (replaces allow_any_sender_id boolean).

ALTER TABLE clients
    ADD COLUMN IF NOT EXISTS allowed_sender_ids_mode TEXT NOT NULL DEFAULT 'list';

UPDATE clients
SET allowed_sender_ids_mode = CASE
    WHEN allow_any_sender_id THEN 'any'
    ELSE 'list'
END
WHERE allowed_sender_ids_mode = 'list' AND allow_any_sender_id IS NOT NULL;

ALTER TABLE clients DROP CONSTRAINT IF EXISTS chk_clients_allowed_sender_ids_mode;
ALTER TABLE clients ADD CONSTRAINT chk_clients_allowed_sender_ids_mode
    CHECK (allowed_sender_ids_mode IN ('list', 'phone', 'any'));

ALTER TABLE clients DROP COLUMN IF EXISTS allow_any_sender_id;

INSERT INTO system_settings (key, value, description)
VALUES (
    'sender_id_any_allowed_pattern',
    '^[A-Za-z0-9 .-]{1,15}$',
    'Regex for sender IDs when client policy is Any (letters, digits, space, dot, hyphen; max 15 chars)'
)
ON CONFLICT (key) DO NOTHING;

-- >>> 007_sender_id_any_pattern_spaces.up.sql <<<
-- Allow spaces in "Any" sender IDs (e.g. "IZ tech").
UPDATE system_settings
SET value = '^[A-Za-z0-9 .-]{1,15}$',
    description = 'Regex for sender IDs when client policy is Any (letters, digits, space, dot, hyphen; max 15 chars)'
WHERE key = 'sender_id_any_allowed_pattern'
  AND value IN (
    '^[A-Za-z0-9]{1,11}$',
    '^[A-Za-z0-9]{1,15}$'
  );

-- >>> 008_admin_users.up.sql <<<
-- Admin accounts with granular permissions; sessions tied to admin_user_id.

CREATE TABLE admin_users (
    admin_user_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username        TEXT        NOT NULL UNIQUE,
    password_hash   TEXT        NOT NULL,
    display_name    TEXT        NOT NULL DEFAULT '',
    email           TEXT,
    phone           TEXT,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    is_super_admin  BOOLEAN     NOT NULL DEFAULT FALSE,
    permissions     JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at   TIMESTAMPTZ
);

CREATE INDEX idx_admin_users_username ON admin_users (username);
CREATE INDEX idx_admin_users_active ON admin_users (is_active) WHERE is_active = TRUE;

ALTER TABLE admin_sessions
    ADD COLUMN admin_user_id UUID REFERENCES admin_users (admin_user_id) ON DELETE CASCADE;

CREATE INDEX idx_admin_sessions_admin_user ON admin_sessions (admin_user_id);

-- Legacy sessions without admin_user_id are invalid after this migration (re-login required).

-- >>> 009_audit_log_admin_user.up.sql <<<
-- Record which admin user performed each audit action.

ALTER TABLE audit_log
    ADD COLUMN admin_user_id UUID REFERENCES admin_users (admin_user_id) ON DELETE SET NULL;

CREATE INDEX idx_audit_log_admin_user ON audit_log (admin_user_id);

-- >>> 010_ledger_immutability_delete.up.sql <<<
-- Block DELETE on append-only financial and audit tables (UPDATE already denied in 001).

CREATE OR REPLACE FUNCTION deny_ledger_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries rows are immutable; DELETE is not permitted';
END;
$$;

CREATE TRIGGER trg_ledger_entries_no_delete
    BEFORE DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION deny_ledger_delete();

CREATE OR REPLACE FUNCTION deny_carrier_balance_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'carrier_balance_entries rows are immutable; DELETE is not permitted';
END;
$$;

CREATE TRIGGER trg_carrier_balance_entries_no_delete
    BEFORE DELETE ON carrier_balance_entries FOR EACH ROW EXECUTE FUNCTION deny_carrier_balance_delete();

CREATE OR REPLACE FUNCTION deny_audit_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log rows are immutable; DELETE is not permitted';
END;
$$;

CREATE TRIGGER trg_audit_log_no_delete
    BEFORE DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION deny_audit_delete();

-- >>> 011_sender_id_any_pattern_underscore.up.sql <<<
-- Allow underscore in "Any" client sender IDs (e.g. IZ_tech).
UPDATE system_settings
SET value = '^[A-Za-z0-9 _.-]{1,15}$',
    description = 'Regex for sender IDs when client policy is Any (letters, digits, space, underscore, dot, hyphen; max 15 chars)'
WHERE key = 'sender_id_any_allowed_pattern';

-- >>> 012_sms_log_event_timeline.up.sql <<<
ALTER TABLE sms_logs
    ADD COLUMN IF NOT EXISTS event_timeline JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_sms_logs_event_timeline_nonempty
    ON sms_logs ((jsonb_array_length(event_timeline)))
    WHERE jsonb_array_length(event_timeline) > 0;

-- >>> 013_invoices.up.sql <<<
CREATE SEQUENCE IF NOT EXISTS invoice_number_seq START 1;

CREATE TABLE IF NOT EXISTS invoices (
    invoice_id        UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type       TEXT            NOT NULL,
    entity_id         UUID            NOT NULL,
    invoice_number    TEXT            NOT NULL UNIQUE,
    invoice_date      DATE            NOT NULL DEFAULT CURRENT_DATE,
    from_date         DATE            NOT NULL,
    to_date           DATE            NOT NULL,
    total_records     INTEGER         NOT NULL DEFAULT 0 CHECK (total_records >= 0),
    total_amount      NUMERIC(18,6)   NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
    pending_amount    NUMERIC(18,6)   NOT NULL DEFAULT 0 CHECK (pending_amount >= 0),
    status            TEXT            NOT NULL DEFAULT 'pending',
    currency          CHAR(3)         NOT NULL DEFAULT 'USD',
    pdf_path          TEXT            NOT NULL,
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_invoices_entity_type CHECK (entity_type IN ('client', 'carrier')),
    CONSTRAINT chk_invoices_status CHECK (status IN ('pending', 'paid', 'partially_paid')),
    CONSTRAINT chk_invoices_date_range CHECK (from_date <= to_date)
);

CREATE INDEX IF NOT EXISTS idx_invoices_entity_date
    ON invoices (entity_type, entity_id, invoice_date DESC, created_at DESC);

INSERT INTO system_settings (key, value, description)
VALUES (
    'invoice_header_image',
    'assets/invoice_header.png',
    'Relative path (under MiniSMS working directory) to invoice header image; rendered 210mm wide on page 1'
)
ON CONFLICT (key) DO NOTHING;

-- >>> 014_client_dlr_webhook_templates.up.sql <<<
ALTER TABLE clients
    ADD COLUMN IF NOT EXISTS dlr_webhook_method TEXT NOT NULL DEFAULT 'POST',
    ADD COLUMN IF NOT EXISTS dlr_webhook_query_template TEXT DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS dlr_webhook_body_template TEXT DEFAULT NULL;

ALTER TABLE clients DROP CONSTRAINT IF EXISTS chk_clients_dlr_webhook_method;
ALTER TABLE clients ADD CONSTRAINT chk_clients_dlr_webhook_method
    CHECK (dlr_webhook_method IN ('GET', 'POST'));
