package inventory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/google/uuid"
)

// MaxReportDays bounds one report's period.
const MaxReportDays = 366

// GrossProfitQuery is the report period [From, To) and the client's UTC
// offset, used only to bucket days.
type GrossProfitQuery struct {
	From            time.Time
	To              time.Time
	TZOffsetMinutes int
}

// GrossProfitFigures are the report's measures (spec §7.2).
type GrossProfitFigures struct {
	RevenueVND       int64    `json:"revenue_vnd"`
	CostVND          float64  `json:"cost_vnd"`
	GrossProfitVND   float64  `json:"gross_profit_vnd"`
	GrossMarginPct   *float64 `json:"gross_margin_pct"`
	WasteCostVND     float64  `json:"waste_cost_vnd"`
	Units            int      `json:"units"`
	UnitsCostNone    int      `json:"units_cost_none"`
	UnitsCostPartial int      `json:"units_cost_partial"`
}

// GrossProfitSize is one Size row (SizeName nil for unsized items).
type GrossProfitSize struct {
	SizeName *string            `json:"size_name"`
	Figures  GrossProfitFigures `json:"figures"`
}

// GrossProfitItem is one Menu Item row with its Size breakdown.
type GrossProfitItem struct {
	MenuItemID uuid.UUID          `json:"menu_item_id"`
	ItemName   string             `json:"item_name"`
	Figures    GrossProfitFigures `json:"figures"`
	Sizes      []GrossProfitSize  `json:"sizes"`
}

// GrossProfitDay is one local day.
type GrossProfitDay struct {
	Date    string             `json:"date"`
	Figures GrossProfitFigures `json:"figures"`
}

// GrossProfitResponse is spec §7.2.
type GrossProfitResponse struct {
	From              time.Time           `json:"from"`
	To                time.Time           `json:"to"`
	Totals            GrossProfitFigures  `json:"totals"`
	Items             []GrossProfitItem   `json:"items"`
	Days              []GrossProfitDay    `json:"days"`
	DisabledIntervals []features.Interval `json:"disabled_intervals"`
}

type reportUnit struct {
	MenuItemID uuid.UUID
	ItemName   string
	SizeName   *string
	RecordedAt time.Time
	CostStatus string
	CostVND    float64
	Remake     bool
	Wasted     bool
	PriceVND   int64
	CompVND    int64
}

// revenue is the unit price less its Comp; a Remake earns nothing.
func (u reportUnit) revenue() int64 {
	if u.Remake {
		return 0
	}
	if r := u.PriceVND - u.CompVND; r > 0 {
		return r
	}
	return 0
}

func (f *GrossProfitFigures) add(u reportUnit) {
	f.RevenueVND += u.revenue()
	f.CostVND += u.CostVND
	if u.Wasted {
		f.WasteCostVND += u.CostVND
	}
	f.Units++
	switch u.CostStatus {
	case CostNone:
		f.UnitsCostNone++
	case CostPartial:
		f.UnitsCostPartial++
	}
}

func (f *GrossProfitFigures) finish() {
	f.CostVND = Round2(f.CostVND)
	f.WasteCostVND = Round2(f.WasteCostVND)
	f.GrossProfitVND = Round2(float64(f.RevenueVND) - f.CostVND)
	f.GrossMarginPct = marginPct(f.RevenueVND, f.CostVND)
}

func sizeKey(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func aggregateGrossProfit(units []reportUnit, loc *time.Location) (GrossProfitFigures, []GrossProfitItem, []GrossProfitDay) {
	var totals GrossProfitFigures
	items := map[uuid.UUID]*GrossProfitItem{}
	sizes := map[uuid.UUID]map[string]*GrossProfitSize{}
	days := map[string]*GrossProfitDay{}
	for _, u := range units {
		totals.add(u)
		it, ok := items[u.MenuItemID]
		if !ok {
			it = &GrossProfitItem{MenuItemID: u.MenuItemID, ItemName: u.ItemName}
			items[u.MenuItemID] = it
			sizes[u.MenuItemID] = map[string]*GrossProfitSize{}
		}
		it.Figures.add(u)
		sz, ok := sizes[u.MenuItemID][sizeKey(u.SizeName)]
		if !ok {
			sz = &GrossProfitSize{SizeName: u.SizeName}
			sizes[u.MenuItemID][sizeKey(u.SizeName)] = sz
		}
		sz.Figures.add(u)
		date := u.RecordedAt.In(loc).Format("2006-01-02")
		d, ok := days[date]
		if !ok {
			d = &GrossProfitDay{Date: date}
			days[date] = d
		}
		d.Figures.add(u)
	}
	totals.finish()

	outItems := make([]GrossProfitItem, 0, len(items))
	for id, it := range items {
		it.Figures.finish()
		it.Sizes = []GrossProfitSize{}
		for _, sz := range sizes[id] {
			sz.Figures.finish()
			it.Sizes = append(it.Sizes, *sz)
		}
		sort.Slice(it.Sizes, func(i, j int) bool { return sizeKey(it.Sizes[i].SizeName) < sizeKey(it.Sizes[j].SizeName) })
		outItems = append(outItems, *it)
	}
	sort.Slice(outItems, func(i, j int) bool { return outItems[i].ItemName < outItems[j].ItemName })

	outDays := make([]GrossProfitDay, 0, len(days))
	for _, d := range days {
		d.Figures.finish()
		outDays = append(outDays, *d)
	}
	sort.Slice(outDays, func(i, j int) bool { return outDays[i].Date < outDays[j].Date })
	return totals, outItems, outDays
}

// ValidateGrossProfitQuery checks the period and offset.
func ValidateGrossProfitQuery(q GrossProfitQuery) error {
	if !q.From.Before(q.To) {
		return fmt.Errorf("%w: from must be before to", ErrInvalidPeriod)
	}
	if q.To.Sub(q.From) > MaxReportDays*24*time.Hour {
		return fmt.Errorf("%w: at most %d days", ErrInvalidPeriod, MaxReportDays)
	}
	if q.TZOffsetMinutes < -720 || q.TZOffsetMinutes > 840 {
		return fmt.Errorf("%w: tz_offset_minutes out of range", ErrInvalidPeriod)
	}
	return nil
}

func sqlcGrossProfitParams(q GrossProfitQuery) sqlc.InventoryListGrossProfitUnitsParams {
	return sqlc.InventoryListGrossProfitUnitsParams{FromAt: q.From, ToAt: q.To}
}

// GrossProfit returns spec §7.2 for the period.
func (r *Reads) GrossProfit(ctx context.Context, actor Actor, query GrossProfitQuery) (GrossProfitResponse, error) {
	if err := ValidateGrossProfitQuery(query); err != nil {
		return GrossProfitResponse{}, err
	}
	return executeRead(ctx, r.runner, actor, CapViewCosts, func(rc ReadContext) (GrossProfitResponse, error) {
		q := rc.Queries
		rows, err := q.InventoryListGrossProfitUnits(ctx, sqlcGrossProfitParams(query))
		if err != nil {
			return GrossProfitResponse{}, fmt.Errorf("list gross profit units: %w", err)
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.PreparationUnitID
		}
		comps := map[uuid.UUID]int64{}
		if len(ids) > 0 {
			compRows, err := q.InventoryListUnitComps(ctx, ids)
			if err != nil {
				return GrossProfitResponse{}, fmt.Errorf("list unit comps: %w", err)
			}
			for _, c := range compRows {
				comps[c.PreparationUnitID.UUID] += c.AmountVnd
			}
		}
		units := make([]reportUnit, 0, len(rows))
		for _, row := range rows {
			u := reportUnit{
				MenuItemID: row.MenuItemID, ItemName: row.ItemName, RecordedAt: row.RecordedAt,
				CostStatus: row.CostStatus, CostVND: row.CostTotalVnd,
				Remake: row.Priority == "REMAKE", Wasted: row.State == "WASTED",
				PriceVND: row.UnitPriceVnd, CompVND: comps[row.PreparationUnitID],
			}
			if row.SizeName.Valid {
				s := row.SizeName.String
				u.SizeName = &s
			}
			units = append(units, u)
		}
		loc := time.FixedZone("client", query.TZOffsetMinutes*60)
		totals, items, days := aggregateGrossProfit(units, loc)
		off, err := features.OffIntervals(ctx, q, FeatureKey, query.From, query.To)
		if err != nil {
			return GrossProfitResponse{}, err
		}
		return GrossProfitResponse{From: query.From, To: query.To, Totals: totals,
			Items: items, Days: days, DisabledIntervals: off}, nil
	})
}
