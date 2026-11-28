package features

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOffIntervals(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }

	cases := []struct {
		name    string
		changes []change
		want    []Interval
	}{
		{"never enabled is off throughout", nil, []Interval{{h(0), h(10)}}},
		{"enabled before the period is on throughout",
			[]change{{true, h(-5)}}, []Interval{}},
		{"enabled inside the period",
			[]change{{true, h(3)}}, []Interval{{h(0), h(3)}}},
		{"on, off, on inside the period",
			[]change{{true, h(-1)}, {false, h(2)}, {true, h(4)}}, []Interval{{h(2), h(4)}}},
		{"turned off and left off",
			[]change{{true, h(-1)}, {false, h(7)}}, []Interval{{h(7), h(10)}}},
		{"enabled exactly at from",
			[]change{{true, h(0)}}, []Interval{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, offIntervals(tc.changes, h(0), h(10)))
		})
	}
}
