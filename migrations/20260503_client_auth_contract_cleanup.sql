-- Client auth contract cleanup.
-- Run after fixing any existing licenses whose customer_id is NULL/empty.

-- Existing data check: this should return 0 before enforcing required customer_id in product flow.
SELECT COUNT(*) AS licenses_without_customer
FROM licenses
WHERE customer_id IS NULL OR customer_id = '';

SELECT tenant_id, license_id, machine_id, COUNT(*) AS duplicate_count
FROM devices
WHERE license_id IS NOT NULL AND license_id != ''
GROUP BY tenant_id, license_id, machine_id
HAVING COUNT(*) > 1;

SELECT tenant_id, subscription_id, machine_id, COUNT(*) AS duplicate_count
FROM devices
WHERE subscription_id IS NOT NULL AND subscription_id != ''
GROUP BY tenant_id, subscription_id, machine_id
HAVING COUNT(*) > 1;

-- Prevent duplicate device rows for the same entitlement and machine.
-- MySQL unique indexes allow multiple NULL values, so license and subscription modes need separate indexes.
CREATE UNIQUE INDEX idx_devices_license_machine
ON devices (tenant_id, license_id, machine_id);

CREATE UNIQUE INDEX idx_devices_subscription_machine
ON devices (tenant_id, subscription_id, machine_id);

-- Heartbeats now record which client auth mode produced the row.
-- Existing rows are license-mode rows because old subscription heartbeats wrote an empty license_id.
ALTER TABLE heartbeats
ADD COLUMN auth_mode VARCHAR(20) NOT NULL DEFAULT 'license' AFTER tenant_id,
ADD COLUMN subscription_id VARCHAR(36) NULL AFTER license_id;

UPDATE heartbeats
SET auth_mode = 'license'
WHERE auth_mode IS NULL OR auth_mode = '';

UPDATE heartbeats h
JOIN devices d ON d.id = h.device_id AND d.tenant_id = h.tenant_id
SET h.auth_mode = 'subscription',
    h.subscription_id = d.subscription_id
WHERE (h.license_id IS NULL OR h.license_id = '')
  AND d.subscription_id IS NOT NULL
  AND d.subscription_id != '';

CREATE INDEX idx_heartbeats_auth_mode ON heartbeats (auth_mode);
CREATE INDEX idx_heartbeats_subscription_id ON heartbeats (subscription_id);
