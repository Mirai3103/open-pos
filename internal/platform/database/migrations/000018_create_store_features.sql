-- 000018: Store Features. A store-wide, Manager-controlled switch that turns
-- an extension outside the Core POS on or off for every client at once.

CREATE TABLE IF NOT EXISTS store_features (
    key TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by_staff_identity_id UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    CONSTRAINT store_feature_key_valid CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$')
);

-- Append-only history of every change; the gross-profit report reads it.
CREATE TABLE IF NOT EXISTS store_feature_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT NOT NULL REFERENCES store_features(key) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS store_feature_change_key_time_index
    ON store_feature_changes (key, changed_at);

INSERT INTO store_features (key, enabled) VALUES ('inventory', false)
    ON CONFLICT (key) DO NOTHING;
