package inventory

import (
	"context"
	"database/sql"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Actor identifies the authenticated staff member.
type Actor = command.Actor

// Runner executes Inventory commands and reads.
type Runner = command.Runner

// Shared pipeline types.
type (
	MutationContext = command.MutationContext
	AuditRecord     = command.AuditRecord
	ReadContext     = command.ReadContext
)

var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "inventory",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

// NewRunner creates a Runner bound to the Inventory policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// executeMutation runs an Inventory mutation; every one needs
// inventory.administer.
func executeMutation[T any](ctx context.Context, r *Runner, actor Actor, requestID uuid.UUID,
	op string, fingerprint any, fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, command.MutationSpec{
		RequestID: requestID, Operation: op, Fingerprint: fingerprint,
		Required: []string{CapAdminister},
	}, fn)
}

// executeRead runs an Inventory read requiring one capability.
func executeRead[T any](ctx context.Context, r *Runner, actor Actor, required string,
	fn func(ReadContext) (T, error),
) (T, error) {
	return command.ExecuteRead(ctx, r, actor, command.ReadSpec{Required: []string{required}}, fn)
}
