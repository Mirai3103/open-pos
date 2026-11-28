// Package features is the Core POS's Store Feature switch: a store-wide,
// Manager-controlled on/off for an extension outside the Core POS. It knows no
// extension; keys are plain strings each extension owns and a migration seeds.
package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
)

// Capability, operation and audit names.
const (
	CapAdminister            = "features.administer"
	OpSetFeature             = "features.set_feature"
	EventFeatureEnabled      = "STORE_FEATURE_ENABLED"
	EventFeatureDisabled     = "STORE_FEATURE_DISABLED"
	EventAuthorizationDenied = "features.authorization_denied"
)

// Sentinel errors.
var (
	ErrUnknownFeature  = errors.New("unknown store feature")
	ErrFeatureDisabled = errors.New("store feature disabled")
)

var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "features",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

// NewRunner creates a Runner bound to the features policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *command.Runner {
	return command.NewRunner(db, queries, policy)
}

// Enabled reports whether key is on, reading inside the caller's transaction
// when q is transactional. An unknown key is off.
func Enabled(ctx context.Context, q *sqlc.Queries, key string) (bool, error) {
	on, err := q.GetStoreFeatureEnabled(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read store feature %q: %w", key, err)
	}
	return on, nil
}
