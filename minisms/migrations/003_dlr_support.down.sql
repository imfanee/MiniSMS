DROP INDEX IF EXISTS idx_sms_logs_dlr_status;
DROP INDEX IF EXISTS idx_sms_logs_dlr_requested;

ALTER TABLE sms_logs
    DROP COLUMN IF EXISTS dest_addr_npi,
    DROP COLUMN IF EXISTS dest_addr_ton,
    DROP COLUMN IF EXISTS source_addr_npi,
    DROP COLUMN IF EXISTS source_addr_ton,
    DROP COLUMN IF EXISTS dlr_forward_attempts,
    DROP COLUMN IF EXISTS dlr_forward_status,
    DROP COLUMN IF EXISTS dlr_forwarded_at,
    DROP COLUMN IF EXISTS dlr_received_at,
    DROP COLUMN IF EXISTS dlr_status,
    DROP COLUMN IF EXISTS dlr_webhook_url,
    DROP COLUMN IF EXISTS dlr_requested;

ALTER TABLE carriers
    DROP COLUMN IF EXISTS smpp_dest_addr_npi,
    DROP COLUMN IF EXISTS smpp_dest_addr_ton,
    DROP COLUMN IF EXISTS smpp_source_addr_npi,
    DROP COLUMN IF EXISTS smpp_source_addr_ton,
    DROP COLUMN IF EXISTS dlr_status_map,
    DROP COLUMN IF EXISTS dlr_status_field,
    DROP COLUMN IF EXISTS dlr_message_id_field,
    DROP COLUMN IF EXISTS dlr_inbound_secret,
    DROP COLUMN IF EXISTS dlr_field_name,
    DROP COLUMN IF EXISTS dlr_callback_url_template;

ALTER TABLE clients
    DROP COLUMN IF EXISTS dlr_webhook_secret,
    DROP COLUMN IF EXISTS dlr_webhook_url;
