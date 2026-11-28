-- name: ListStoreFeatures :many
SELECT key, enabled FROM store_features ORDER BY key;

-- name: GetStoreFeatureEnabled :one
SELECT enabled FROM store_features WHERE key = $1;

-- name: LockStoreFeature :one
SELECT key, enabled FROM store_features WHERE key = $1 FOR UPDATE;

-- name: SetStoreFeatureEnabled :exec
UPDATE store_features
SET enabled = sqlc.arg(enabled), updated_at = now(),
    updated_by_staff_identity_id = sqlc.arg(actor_id)::uuid
WHERE key = sqlc.arg(key);

-- name: InsertStoreFeatureChange :exec
INSERT INTO store_feature_changes (key, enabled, actor_staff_identity_id)
VALUES ($1, $2, $3);

-- name: ListStoreFeatureChanges :many
SELECT enabled, changed_at FROM store_feature_changes
WHERE key = sqlc.arg(key) AND changed_at < sqlc.arg(until)::timestamptz
ORDER BY changed_at, id;
