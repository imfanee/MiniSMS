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
