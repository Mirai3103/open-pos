package features

import (
	"context"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
)

// Interval is a half-open time range [From, To).
type Interval struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type change struct {
	enabled bool
	at      time.Time
}

// OffIntervals returns the parts of [from, to) during which key was off. A key
// with no change before from was off then, because every feature is seeded
// off.
func OffIntervals(ctx context.Context, q *sqlc.Queries, key string, from, to time.Time) ([]Interval, error) {
	rows, err := q.ListStoreFeatureChanges(ctx, sqlc.ListStoreFeatureChangesParams{Key: key, Until: to})
	if err != nil {
		return nil, fmt.Errorf("list store feature changes: %w", err)
	}
	changes := make([]change, 0, len(rows))
	for _, r := range rows {
		changes = append(changes, change{enabled: r.Enabled, at: r.ChangedAt})
	}
	return offIntervals(changes, from, to), nil
}

func offIntervals(changes []change, from, to time.Time) []Interval {
	enabled := false
	i := 0
	for ; i < len(changes) && !changes[i].at.After(from); i++ {
		enabled = changes[i].enabled
	}
	out := []Interval{}
	var offStart *time.Time
	if !enabled {
		start := from
		offStart = &start
	}
	for ; i < len(changes) && changes[i].at.Before(to); i++ {
		c := changes[i]
		switch {
		case c.enabled && offStart != nil:
			if c.at.After(*offStart) {
				out = append(out, Interval{From: *offStart, To: c.at})
			}
			offStart = nil
		case !c.enabled && offStart == nil:
			start := c.at
			offStart = &start
		}
	}
	if offStart != nil && to.After(*offStart) {
		out = append(out, Interval{From: *offStart, To: to})
	}
	return out
}
