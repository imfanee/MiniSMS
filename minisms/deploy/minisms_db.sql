-- =============================================================================
-- MiniSMS — Complete Database Schema
-- Generated: 2026-04-26
-- PostgreSQL 15+
--
-- Usage (fresh installation):
--   psql -h localhost -U postgres -d minisms -f deploy/minisms_db.sql
--
-- For existing deployments using golang-migrate, continue using:
--   make migrate
--
-- This file is the single source of truth for the complete schema.
-- It is idempotent — safe to run on an existing database.
-- =============================================================================

-- 1) Extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- 2) Tables (dependency order)
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

CREATE TABLE IF NOT EXISTS admin_sessions (
    session_id      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_token   TEXT        NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip_address      INET,
    user_agent      TEXT,
    is_revoked      BOOLEAN     NOT NULL DEFAULT FALSE
);

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

CREATE TABLE IF NOT EXISTS rate_groups (
    rate_group_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT        NOT NULL UNIQUE,
    currency        CHAR(3)     NOT NULL REFERENCES currencies(code) ON UPDATE CASCADE,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_rate_groups_currency CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE TABLE IF NOT EXISTS rate_entries (
    rate_entry_id   UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    rate_group_id   UUID            NOT NULL REFERENCES rate_groups (rate_group_id) ON DELETE RESTRICT,
    prefix          TEXT            NOT NULL,
    description     TEXT,
    rate_per_sms    NUMERIC(18,6)   NOT NULL CHECK (rate_per_sms >= 0),
    effective_from  DATE            NOT NULL,
    effective_to    DATE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_rate_entries_prefix CHECK (prefix = '*' OR prefix ~ '^[0-9]+$'),
    CONSTRAINT chk_rate_entries_dates CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT uq_rate_entries_group_prefix_from UNIQUE (rate_group_id, prefix, effective_from)
);

CREATE TABLE IF NOT EXISTS carriers (
    carrier_id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name                        TEXT            NOT NULL UNIQUE,
    endpoint_url                TEXT            NOT NULL,
    http_method                 TEXT            NOT NULL DEFAULT 'POST',
    status                      TEXT            NOT NULL DEFAULT 'active',
    rate_group_id               UUID            REFERENCES rate_groups(rate_group_id) ON DELETE SET NULL,
    balance                     NUMERIC(18,6)   NOT NULL DEFAULT 0,
    currency                    CHAR(3)         NOT NULL DEFAULT 'GBP' REFERENCES currencies(code) ON UPDATE CASCADE,
    notes                       TEXT,
    sender_id_policy            TEXT            NOT NULL DEFAULT 'any',
    default_sender_id_value     TEXT,
    dlr_callback_url_template   TEXT,
    dlr_field_name              TEXT,
    dlr_inbound_secret          TEXT,
    dlr_message_id_field        TEXT,
    dlr_status_field            TEXT,
    dlr_status_map              JSONB,
    smpp_source_addr_ton        TEXT            NOT NULL DEFAULT 'dynamic',
    smpp_source_addr_npi        TEXT            NOT NULL DEFAULT 'dynamic',
    smpp_dest_addr_ton          TEXT            NOT NULL DEFAULT 'dynamic',
    smpp_dest_addr_npi          TEXT            NOT NULL DEFAULT 'dynamic',
    created_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_carriers_http_method CHECK (http_method IN ('GET', 'POST')),
    CONSTRAINT chk_carriers_status CHECK (status IN ('active', 'inactive')),
    CONSTRAINT chk_carriers_currency CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT chk_carriers_sender_policy CHECK (sender_id_policy IN ('any','numeric','e164','list','none'))
);

CREATE TABLE IF NOT EXISTS carrier_auth_headers (
    header_id        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id       UUID        NOT NULL REFERENCES carriers (carrier_id) ON DELETE CASCADE,
    header_name      TEXT        NOT NULL,
    header_value_enc TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_carrier_auth_headers_name UNIQUE (carrier_id, header_name)
);

CREATE TABLE IF NOT EXISTS carrier_request_templates (
    template_id     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id      UUID        NOT NULL REFERENCES carriers (carrier_id) ON DELETE CASCADE,
    content_type    TEXT        NOT NULL DEFAULT 'application/json',
    body_template   TEXT,
    query_template  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_carrier_request_templates_carrier UNIQUE (carrier_id),
    CONSTRAINT chk_carrier_request_templates_content_type
        CHECK (content_type IN ('application/json','application/x-www-form-urlencoded','text/xml','application/xml'))
);

CREATE TABLE IF NOT EXISTS carrier_usage_totals (
    carrier_id      UUID            PRIMARY KEY REFERENCES carriers (carrier_id) ON DELETE CASCADE,
    total_messages  BIGINT          NOT NULL DEFAULT 0 CHECK (total_messages >= 0),
    total_segments  BIGINT          NOT NULL DEFAULT 0 CHECK (total_segments >= 0),
    total_amount    NUMERIC(18,6)   NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
    last_message_at TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS carrier_balance_entries (
    entry_id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id        UUID            NOT NULL REFERENCES carriers (carrier_id) ON DELETE RESTRICT,
    entry_type        TEXT            NOT NULL,
    amount            NUMERIC(18,6)   NOT NULL CHECK (amount > 0),
    direction         SMALLINT        NOT NULL,
    balance_before    NUMERIC(18,6)   NOT NULL,
    balance_after     NUMERIC(18,6)   NOT NULL,
    currency          CHAR(3)         NOT NULL,
    payment_reference TEXT,
    invoice_number    TEXT,
    payment_date      DATE,
    message_id        UUID,
    notes             TEXT,
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_carrier_balance_entry_type CHECK (entry_type IN ('payment','charge','adjustment','refund')),
    CONSTRAINT chk_carrier_balance_direction CHECK (direction IN (1, -1))
);

CREATE TABLE IF NOT EXISTS carrier_sender_ids (
    carrier_sender_id UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id        UUID        NOT NULL REFERENCES carriers(carrier_id) ON DELETE CASCADE,
    sender_id         UUID        NOT NULL REFERENCES sender_ids(sender_id) ON DELETE RESTRICT,
    is_default        BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_carrier_sender_ids UNIQUE (carrier_id, sender_id)
);

CREATE TABLE IF NOT EXISTS routing_groups (
    routing_group_id    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT        NOT NULL UNIQUE,
    description         TEXT,
    status              TEXT        NOT NULL DEFAULT 'active',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_routing_groups_status CHECK (status IN ('active', 'inactive'))
);

CREATE TABLE IF NOT EXISTS route_entries (
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
    CONSTRAINT uq_route_entries_group_prefix UNIQUE (routing_group_id, prefix),
    CONSTRAINT chk_route_entries_prefix CHECK (prefix = '*' OR prefix ~ '^[0-9]+$'),
    CONSTRAINT chk_route_entries_status CHECK (status IN ('active', 'inactive')),
    CONSTRAINT chk_route_entries_failover_order CHECK (failover2_carrier_id IS NULL OR failover1_carrier_id IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS clients (
    client_id               UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    TEXT            NOT NULL,
    email                   CITEXT          NOT NULL UNIQUE,
    status                  TEXT            NOT NULL DEFAULT 'active',
    rate_group_id           UUID            REFERENCES rate_groups (rate_group_id) ON DELETE SET NULL,
    balance                 NUMERIC(18,6)   NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency                CHAR(3)         NOT NULL REFERENCES currencies(code) ON UPDATE CASCADE,
    routing_group_id        UUID            REFERENCES routing_groups (routing_group_id) ON DELETE SET NULL,
    notes                   TEXT,
    default_sender_id_value TEXT,
    allow_any_sender_id     BOOLEAN         NOT NULL DEFAULT FALSE,
    allow_in_loss_delivery  BOOLEAN         NOT NULL DEFAULT TRUE,
    dlr_webhook_url         TEXT,
    dlr_webhook_secret      TEXT,
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_clients_status CHECK (status IN ('active', 'suspended', 'disabled')),
    CONSTRAINT chk_clients_currency CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE TABLE IF NOT EXISTS client_sender_ids (
    client_sender_id UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id        UUID        NOT NULL REFERENCES clients(client_id) ON DELETE CASCADE,
    sender_id        UUID        NOT NULL REFERENCES sender_ids(sender_id) ON DELETE RESTRICT,
    is_default       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_client_sender_ids UNIQUE (client_id, sender_id)
);

CREATE TABLE IF NOT EXISTS client_api_keys (
    key_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES clients (client_id) ON DELETE CASCADE,
    key_hash        TEXT        NOT NULL UNIQUE,
    key_salt        TEXT        NOT NULL,
    key_prefix      CHAR(8)     NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ,
    revoked_reason  TEXT
);

CREATE TABLE IF NOT EXISTS ledger_entries (
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
    CONSTRAINT chk_ledger_entry_type CHECK (entry_type IN ('credit', 'debit')),
    CONSTRAINT chk_ledger_balance_after CHECK (balance_after >= 0)
);

CREATE TABLE IF NOT EXISTS sms_logs (
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
    sender_id_source        TEXT            NOT NULL DEFAULT 'client_provided',
    carrier_skip_reason     JSONB,
    dlr_requested           BOOLEAN         NOT NULL DEFAULT FALSE,
    dlr_webhook_url         TEXT,
    dlr_status              TEXT,
    dlr_received_at         TIMESTAMPTZ,
    dlr_forwarded_at        TIMESTAMPTZ,
    dlr_forward_status      TEXT,
    dlr_forward_attempts    INT             NOT NULL DEFAULT 0,
    source_addr_ton         SMALLINT,
    source_addr_npi         SMALLINT,
    dest_addr_ton           SMALLINT,
    dest_addr_npi           SMALLINT,
    CONSTRAINT chk_sms_logs_status CHECK (status IN ('pending','accepted','sent','delivered','failed','rejected')),
    CONSTRAINT chk_sms_logs_encoding CHECK (encoding IN ('GSM7', 'UCS2'))
);

CREATE TABLE IF NOT EXISTS audit_log (
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

CREATE TABLE IF NOT EXISTS system_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    description TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 3) Functions
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION deny_ledger_update() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries rows are immutable; UPDATE is not permitted';
END;
$$;

CREATE OR REPLACE FUNCTION deny_carrier_balance_update() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'carrier_balance_entries rows are immutable; UPDATE is not permitted';
END;
$$;

CREATE OR REPLACE FUNCTION deny_audit_update() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log rows are immutable; UPDATE is not permitted';
END;
$$;

CREATE OR REPLACE FUNCTION record_carrier_payment(
    p_carrier_id UUID, p_amount NUMERIC(18,6), p_currency CHAR(3),
    p_payment_reference TEXT DEFAULT NULL, p_invoice_number TEXT DEFAULT NULL,
    p_payment_date DATE DEFAULT CURRENT_DATE, p_notes TEXT DEFAULT NULL
) RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE v_balance_before NUMERIC(18,6); v_balance_after NUMERIC(18,6);
BEGIN
    IF p_amount <= 0 THEN
        RAISE EXCEPTION 'Payment amount must be positive; got %', p_amount USING ERRCODE = 'P0002';
    END IF;
    SELECT balance INTO v_balance_before FROM carriers WHERE carrier_id = p_carrier_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Carrier % not found', p_carrier_id USING ERRCODE = 'P0003';
    END IF;
    v_balance_after := v_balance_before + p_amount;
    UPDATE carriers SET balance = v_balance_after, updated_at = now() WHERE carrier_id = p_carrier_id;
    INSERT INTO carrier_balance_entries (
        carrier_id, entry_type, amount, direction, balance_before, balance_after, currency,
        payment_reference, invoice_number, payment_date, notes
    ) VALUES (
        p_carrier_id, 'payment', p_amount, 1, v_balance_before, v_balance_after, p_currency,
        p_payment_reference, p_invoice_number, p_payment_date, p_notes
    );
    RETURN v_balance_after;
END;
$$;

CREATE OR REPLACE FUNCTION deduct_carrier_balance(
    p_carrier_id UUID, p_amount NUMERIC(18,6), p_currency CHAR(3), p_message_id UUID
) RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE v_balance_before NUMERIC(18,6); v_balance_after NUMERIC(18,6);
BEGIN
    SELECT balance INTO v_balance_before FROM carriers WHERE carrier_id = p_carrier_id FOR UPDATE;
    v_balance_after := v_balance_before - p_amount;
    UPDATE carriers SET balance = v_balance_after, updated_at = now() WHERE carrier_id = p_carrier_id;
    INSERT INTO carrier_balance_entries (
        carrier_id, entry_type, amount, direction, balance_before, balance_after, currency, message_id
    ) VALUES (
        p_carrier_id, 'charge', p_amount, -1, v_balance_before, v_balance_after, p_currency, p_message_id
    );
    RETURN v_balance_after;
END;
$$;

CREATE OR REPLACE FUNCTION increment_carrier_usage(
    p_carrier_id UUID, p_segments INT, p_amount NUMERIC(18,6)
) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO carrier_usage_totals (carrier_id, total_messages, total_segments, total_amount, last_message_at)
    VALUES (p_carrier_id, 1, p_segments, p_amount, now())
    ON CONFLICT (carrier_id) DO UPDATE SET
        total_messages  = carrier_usage_totals.total_messages + 1,
        total_segments  = carrier_usage_totals.total_segments + EXCLUDED.total_segments,
        total_amount    = carrier_usage_totals.total_amount + EXCLUDED.total_amount,
        last_message_at = now(),
        updated_at      = now();
END;
$$;

CREATE OR REPLACE FUNCTION deduct_client_balance(
    p_client_id UUID, p_amount NUMERIC(18,6), p_message_id UUID, p_currency CHAR(3)
) RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE v_balance_before NUMERIC(18,6); v_balance_after NUMERIC(18,6);
BEGIN
    SELECT balance INTO v_balance_before FROM clients WHERE client_id = p_client_id FOR UPDATE;
    IF v_balance_before < p_amount THEN
        RAISE EXCEPTION 'INSUFFICIENT_BALANCE: client % has % but requires %', p_client_id, v_balance_before, p_amount
            USING ERRCODE = 'P0001';
    END IF;
    v_balance_after := v_balance_before - p_amount;
    UPDATE clients SET balance = v_balance_after, updated_at = now() WHERE client_id = p_client_id;
    INSERT INTO ledger_entries (
        client_id, entry_type, amount, balance_before, balance_after, currency, reference, message_id
    ) VALUES (
        p_client_id, 'debit', p_amount, v_balance_before, v_balance_after, p_currency, p_message_id::TEXT, p_message_id
    );
    RETURN v_balance_after;
END;
$$;

CREATE OR REPLACE FUNCTION credit_client_balance(
    p_client_id UUID, p_amount NUMERIC(18,6), p_currency CHAR(3), p_reference TEXT DEFAULT NULL, p_notes TEXT DEFAULT NULL
) RETURNS NUMERIC(18,6) LANGUAGE plpgsql AS $$
DECLARE v_balance_before NUMERIC(18,6); v_balance_after NUMERIC(18,6);
BEGIN
    SELECT balance INTO v_balance_before FROM clients WHERE client_id = p_client_id FOR UPDATE;
    v_balance_after := v_balance_before + p_amount;
    UPDATE clients SET balance = v_balance_after, updated_at = now() WHERE client_id = p_client_id;
    INSERT INTO ledger_entries (
        client_id, entry_type, amount, balance_before, balance_after, currency, reference, notes
    ) VALUES (
        p_client_id, 'credit', p_amount, v_balance_before, v_balance_after, p_currency, p_reference, p_notes
    );
    RETURN v_balance_after;
END;
$$;

-- 4) Triggers
DROP TRIGGER IF EXISTS trg_currencies_updated_at ON currencies;
CREATE TRIGGER trg_currencies_updated_at BEFORE UPDATE ON currencies FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_sender_ids_updated_at ON sender_ids;
CREATE TRIGGER trg_sender_ids_updated_at BEFORE UPDATE ON sender_ids FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_rate_groups_updated_at ON rate_groups;
CREATE TRIGGER trg_rate_groups_updated_at BEFORE UPDATE ON rate_groups FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_rate_entries_updated_at ON rate_entries;
CREATE TRIGGER trg_rate_entries_updated_at BEFORE UPDATE ON rate_entries FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_carriers_updated_at ON carriers;
CREATE TRIGGER trg_carriers_updated_at BEFORE UPDATE ON carriers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_carrier_auth_headers_updated_at ON carrier_auth_headers;
CREATE TRIGGER trg_carrier_auth_headers_updated_at BEFORE UPDATE ON carrier_auth_headers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_carrier_request_templates_updated_at ON carrier_request_templates;
CREATE TRIGGER trg_carrier_request_templates_updated_at BEFORE UPDATE ON carrier_request_templates FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_routing_groups_updated_at ON routing_groups;
CREATE TRIGGER trg_routing_groups_updated_at BEFORE UPDATE ON routing_groups FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_route_entries_updated_at ON route_entries;
CREATE TRIGGER trg_route_entries_updated_at BEFORE UPDATE ON route_entries FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_clients_updated_at ON clients;
CREATE TRIGGER trg_clients_updated_at BEFORE UPDATE ON clients FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_system_settings_updated_at ON system_settings;
CREATE TRIGGER trg_system_settings_updated_at BEFORE UPDATE ON system_settings FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_ledger_entries_no_update ON ledger_entries;
CREATE TRIGGER trg_ledger_entries_no_update BEFORE UPDATE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION deny_ledger_update();
DROP TRIGGER IF EXISTS trg_carrier_balance_entries_no_update ON carrier_balance_entries;
CREATE TRIGGER trg_carrier_balance_entries_no_update BEFORE UPDATE ON carrier_balance_entries FOR EACH ROW EXECUTE FUNCTION deny_carrier_balance_update();
DROP TRIGGER IF EXISTS trg_audit_log_no_update ON audit_log;
CREATE TRIGGER trg_audit_log_no_update BEFORE UPDATE ON audit_log FOR EACH ROW EXECUTE FUNCTION deny_audit_update();

-- 5) Indexes
CREATE INDEX IF NOT EXISTS idx_currencies_active ON currencies (is_active);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_token ON admin_sessions (session_token);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires ON admin_sessions (expires_at);
CREATE INDEX IF NOT EXISTS idx_sender_ids_value ON sender_ids (value);
CREATE INDEX IF NOT EXISTS idx_sender_ids_active ON sender_ids (is_active);
CREATE INDEX IF NOT EXISTS idx_rate_entries_group ON rate_entries (rate_group_id);
CREATE INDEX IF NOT EXISTS idx_rate_entries_prefix ON rate_entries (prefix);
CREATE INDEX IF NOT EXISTS idx_carriers_status ON carriers (status);
CREATE INDEX IF NOT EXISTS idx_carrier_auth_headers_carrier ON carrier_auth_headers (carrier_id);
CREATE INDEX IF NOT EXISTS idx_carrier_request_templates_carrier ON carrier_request_templates (carrier_id);
CREATE INDEX IF NOT EXISTS idx_carrier_balance_entries_carrier ON carrier_balance_entries (carrier_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_carrier_balance_entries_type ON carrier_balance_entries (entry_type);
CREATE INDEX IF NOT EXISTS idx_carrier_balance_entries_message ON carrier_balance_entries (message_id) WHERE message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_route_entries_group ON route_entries (routing_group_id);
CREATE INDEX IF NOT EXISTS idx_route_entries_prefix ON route_entries (prefix);
CREATE INDEX IF NOT EXISTS idx_route_entries_status ON route_entries (routing_group_id, status);
CREATE INDEX IF NOT EXISTS idx_route_entries_primary_carrier ON route_entries (primary_carrier_id);
CREATE INDEX IF NOT EXISTS idx_route_entries_failover1 ON route_entries (failover1_carrier_id) WHERE failover1_carrier_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_route_entries_failover2 ON route_entries (failover2_carrier_id) WHERE failover2_carrier_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_clients_status ON clients (status);
CREATE INDEX IF NOT EXISTS idx_clients_rate_group ON clients (rate_group_id);
CREATE INDEX IF NOT EXISTS idx_clients_routing_group ON clients (routing_group_id);
CREATE INDEX IF NOT EXISTS idx_client_sender_ids_client ON client_sender_ids (client_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_client_sender_ids_default ON client_sender_ids (client_id) WHERE is_default = TRUE;
CREATE INDEX IF NOT EXISTS idx_carrier_sender_ids_carrier ON carrier_sender_ids (carrier_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_carrier_sender_ids_default ON carrier_sender_ids (carrier_id) WHERE is_default = TRUE;
CREATE INDEX IF NOT EXISTS idx_client_api_keys_client ON client_api_keys (client_id);
CREATE INDEX IF NOT EXISTS idx_client_api_keys_hash ON client_api_keys (key_hash);
CREATE UNIQUE INDEX IF NOT EXISTS uq_client_api_keys_active ON client_api_keys (client_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ledger_entries_client ON ledger_entries (client_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_message ON ledger_entries (message_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_type ON ledger_entries (entry_type);
CREATE INDEX IF NOT EXISTS idx_sms_logs_client ON sms_logs (client_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_logs_carrier ON sms_logs (carrier_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_logs_status ON sms_logs (status);
CREATE INDEX IF NOT EXISTS idx_sms_logs_received ON sms_logs (received_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_logs_to_number ON sms_logs (to_number);
CREATE INDEX IF NOT EXISTS idx_sms_logs_client_ref ON sms_logs (client_ref) WHERE client_ref IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sms_logs_routing_group ON sms_logs (routing_group_id);
CREATE INDEX IF NOT EXISTS idx_sms_logs_route_entry ON sms_logs (route_entry_id);
CREATE INDEX IF NOT EXISTS idx_sms_logs_failover ON sms_logs (failover_sequence) WHERE failover_sequence > 0;
CREATE INDEX IF NOT EXISTS idx_sms_logs_sid_source ON sms_logs(sender_id_source);
CREATE INDEX IF NOT EXISTS idx_sms_logs_dlr_requested ON sms_logs (dlr_requested) WHERE dlr_requested = TRUE;
CREATE INDEX IF NOT EXISTS idx_sms_logs_dlr_status ON sms_logs (dlr_status) WHERE dlr_status IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log (action);
CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_session ON audit_log (session_id);

-- 6) Views
CREATE OR REPLACE VIEW v_active_rate_entries AS
SELECT re.rate_entry_id, re.rate_group_id, rg.name AS rate_group_name, rg.currency, re.prefix,
       re.description, re.rate_per_sms, re.effective_from, re.effective_to
FROM rate_entries re
JOIN rate_groups rg ON rg.rate_group_id = re.rate_group_id
WHERE CURRENT_DATE >= re.effective_from
  AND (re.effective_to IS NULL OR CURRENT_DATE <= re.effective_to);

CREATE OR REPLACE VIEW v_active_api_keys AS
SELECT k.key_id, k.client_id, k.key_hash, k.key_salt, k.key_prefix, k.created_at,
       c.name AS client_name, c.status AS client_status, c.balance, c.currency, c.rate_group_id, c.routing_group_id
FROM client_api_keys k
JOIN clients c ON c.client_id = k.client_id
WHERE k.revoked_at IS NULL;

CREATE OR REPLACE VIEW v_routing_group_detail AS
SELECT rg.routing_group_id, rg.name, rg.description, rg.status, rg.created_at, rg.updated_at,
       COUNT(re.route_entry_id) AS total_routes,
       COUNT(re.route_entry_id) FILTER (WHERE re.status = 'active') AS active_routes,
       COUNT(re.route_entry_id) FILTER (WHERE re.failover1_carrier_id IS NOT NULL) AS routes_with_failover1,
       COUNT(re.route_entry_id) FILTER (WHERE re.failover2_carrier_id IS NOT NULL) AS routes_with_failover2
FROM routing_groups rg
LEFT JOIN route_entries re ON re.routing_group_id = rg.routing_group_id
GROUP BY rg.routing_group_id;

CREATE OR REPLACE VIEW v_route_entries_detail AS
SELECT re.route_entry_id, re.routing_group_id, rg.name AS routing_group_name, re.prefix, re.description, re.priority, re.status,
       re.primary_carrier_id, cp.name AS primary_carrier_name, cp.status AS primary_carrier_status, cp.balance AS primary_carrier_balance,
       re.failover1_carrier_id, cf1.name AS failover1_carrier_name, cf1.status AS failover1_carrier_status, cf1.balance AS failover1_carrier_balance,
       re.failover2_carrier_id, cf2.name AS failover2_carrier_name, cf2.status AS failover2_carrier_status, cf2.balance AS failover2_carrier_balance,
       re.created_at, re.updated_at
FROM route_entries re
JOIN routing_groups rg ON rg.routing_group_id = re.routing_group_id
JOIN carriers cp ON cp.carrier_id = re.primary_carrier_id
LEFT JOIN carriers cf1 ON cf1.carrier_id = re.failover1_carrier_id
LEFT JOIN carriers cf2 ON cf2.carrier_id = re.failover2_carrier_id;

CREATE OR REPLACE VIEW v_carrier_financial_position AS
SELECT c.carrier_id, c.name AS carrier_name, c.status, c.balance AS current_balance, c.currency,
       COALESCE(cu.total_messages, 0) AS total_messages_sent,
       COALESCE(cu.total_amount, 0) AS total_amount_charged_all_time,
       cu.last_message_at,
       COALESCE(SUM(cbe.amount) FILTER (WHERE cbe.entry_type = 'charge' AND cbe.created_at >= now() - INTERVAL '30 days'), 0) AS spend_last_30d,
       COALESCE(SUM(cbe.amount) FILTER (WHERE cbe.entry_type = 'payment'), 0) AS total_payments_received,
       COALESCE(SUM(cbe.amount) FILTER (WHERE cbe.entry_type = 'refund'), 0) AS total_refunds_received,
       c.updated_at
FROM carriers c
LEFT JOIN carrier_usage_totals cu ON cu.carrier_id = c.carrier_id
LEFT JOIN carrier_balance_entries cbe ON cbe.carrier_id = c.carrier_id
GROUP BY c.carrier_id, c.name, c.status, c.balance, c.currency, cu.total_messages, cu.total_amount, cu.last_message_at, c.updated_at;

CREATE OR REPLACE VIEW v_client_sms_summary AS
SELECT sl.client_id, c.name AS client_name, c.routing_group_id, rg.name AS routing_group_name,
       COUNT(*) AS total_messages, SUM(sl.segments) AS total_segments, SUM(sl.total_charged) AS total_charged,
       COUNT(*) FILTER (WHERE sl.status = 'delivered') AS delivered_count,
       COUNT(*) FILTER (WHERE sl.status = 'failed') AS failed_count,
       COUNT(*) FILTER (WHERE sl.status = 'rejected') AS rejected_count,
       COUNT(*) FILTER (WHERE sl.failover_sequence = 1) AS failover1_used_count,
       COUNT(*) FILTER (WHERE sl.failover_sequence = 2) AS failover2_used_count,
       MAX(sl.received_at) AS last_message_at
FROM sms_logs sl
JOIN clients c ON c.client_id = sl.client_id
LEFT JOIN routing_groups rg ON rg.routing_group_id = c.routing_group_id
WHERE sl.received_at >= now() - INTERVAL '30 days'
GROUP BY sl.client_id, c.name, c.routing_group_id, rg.name;

CREATE OR REPLACE VIEW v_carrier_sms_summary AS
SELECT sl.carrier_id, c.name AS carrier_name, COUNT(*) AS total_messages, SUM(sl.segments) AS total_segments,
       COUNT(*) FILTER (WHERE sl.failover_sequence = 0) AS dispatched_as_primary,
       COUNT(*) FILTER (WHERE sl.failover_sequence = 1) AS dispatched_as_failover1,
       COUNT(*) FILTER (WHERE sl.failover_sequence = 2) AS dispatched_as_failover2,
       COUNT(*) FILTER (WHERE sl.status = 'failed') AS failed_count,
       ROUND(COUNT(*) FILTER (WHERE sl.status IN ('sent', 'delivered'))::NUMERIC / NULLIF(COUNT(*), 0) * 100, 2) AS success_rate_pct,
       MAX(sl.dispatched_at) AS last_dispatched_at
FROM sms_logs sl
JOIN carriers c ON c.carrier_id = sl.carrier_id
WHERE sl.received_at >= now() - INTERVAL '30 days'
  AND sl.carrier_id IS NOT NULL
GROUP BY sl.carrier_id, c.name;

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
FROM sms_logs sl
JOIN carriers c ON c.carrier_id = sl.carrier_id
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

-- 7) Seed data
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

-- 8) Role and grants (operator-applied)
-- CREATE ROLE minisms_app LOGIN PASSWORD 'CHANGE_ME';
-- GRANT SELECT, INSERT, UPDATE ON admin_sessions, rate_groups, rate_entries, routing_groups, route_entries,
--   carriers, carrier_auth_headers, carrier_request_templates, carrier_usage_totals, clients, client_api_keys,
--   sms_logs, system_settings TO minisms_app;
-- GRANT SELECT, INSERT ON ledger_entries, carrier_balance_entries, audit_log TO minisms_app;
-- GRANT EXECUTE ON FUNCTION record_carrier_payment(UUID, NUMERIC, CHAR, TEXT, TEXT, DATE, TEXT),
--   deduct_carrier_balance(UUID, NUMERIC, CHAR, UUID),
--   increment_carrier_usage(UUID, INT, NUMERIC),
--   deduct_client_balance(UUID, NUMERIC, UUID, CHAR),
--   credit_client_balance(UUID, NUMERIC, CHAR, TEXT, TEXT) TO minisms_app;
-- GRANT SELECT ON v_active_rate_entries, v_active_api_keys, v_routing_group_detail, v_route_entries_detail,
--   v_carrier_financial_position, v_client_sms_summary, v_carrier_sms_summary, v_client_sender_ids,
--   v_carrier_sender_ids, v_carrier_prefix_success, v_client_bill_vs_carrier_cost TO minisms_app;

