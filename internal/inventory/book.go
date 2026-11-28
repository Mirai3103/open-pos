package inventory

import "github.com/google/uuid"

// Line is one recipe line normalized to the Ingredient's Stock Unit.
type Line struct {
	IngredientID uuid.UUID
	StockQty     float64
}

// Batch is a prepared Ingredient's Batch Recipe; Yield is in its Stock Units.
type Batch struct {
	Lines []Line
	Yield float64
}

// Ingredient is what composition needs to know about an Ingredient.
type Ingredient struct {
	ID        uuid.UUID
	Name      string
	Kind      string
	StockUnit string
	Retired   bool
}

// Book is every Ingredient, current raw cost and Recipe, in memory. It is
// loaded per use (LoadBook); a single cafe's book is a few hundred rows.
type Book struct {
	Ingredients map[uuid.UUID]Ingredient
	RawCosts    map[uuid.UUID]float64
	Items       map[uuid.UUID][]Line
	Sizes       map[uuid.UUID][]Line
	Options     map[uuid.UUID][]Line
	Batches     map[uuid.UUID]Batch
}

// NewBook returns an empty Book.
func NewBook() *Book {
	return &Book{
		Ingredients: map[uuid.UUID]Ingredient{},
		RawCosts:    map[uuid.UUID]float64{},
		Items:       map[uuid.UUID][]Line{},
		Sizes:       map[uuid.UUID][]Line{},
		Options:     map[uuid.UUID][]Line{},
		Batches:     map[uuid.UUID]Batch{},
	}
}

// UnitCost returns the cost of one Stock Unit, and false when it has none: a
// raw Ingredient with no cost entry, or a prepared one without a Batch Recipe
// or with an uncosted raw input.
func (b *Book) UnitCost(id uuid.UUID) (float64, bool) {
	ing, ok := b.Ingredients[id]
	if !ok {
		return 0, false
	}
	if ing.Kind == KindRaw {
		c, ok := b.RawCosts[id]
		return c, ok
	}
	batch, ok := b.Batches[id]
	if !ok || batch.Yield <= 0 || len(batch.Lines) == 0 {
		return 0, false
	}
	total := 0.0
	for _, l := range batch.Lines {
		c, ok := b.RawCosts[l.IngredientID]
		if !ok {
			return 0, false
		}
		total += l.StockQty * c
	}
	perUnit := Round4(total / batch.Yield)
	if perUnit > MaxCostPerStockUnit {
		return 0, false // out of range: treat as uncosted rather than overflow a column
	}
	return perUnit, true
}

// Selection is what one Preparation Unit was sold as.
type Selection struct {
	MenuItemID        uuid.UUID
	SizeID            *uuid.UUID
	ModifierOptionIDs []uuid.UUID
}

// ComposedLine is one consumed Ingredient. UnitCost and LineCost are nil when
// the Ingredient had no cost.
type ComposedLine struct {
	IngredientID uuid.UUID
	Name         string
	Kind         string
	StockUnit    string
	StockQty     float64
	UnitCost     *float64
	LineCost     *float64
}

// Composition is what one Preparation Unit consumed and cost (spec §3.5).
type Composition struct {
	Lines    []ComposedLine
	TotalVND float64
	Status   string
}

func (b *Book) baseLines(sel Selection) ([]Line, bool) {
	if sel.SizeID != nil {
		l, ok := b.Sizes[*sel.SizeID]
		return l, ok && len(l) > 0
	}
	l, ok := b.Items[sel.MenuItemID]
	return l, ok && len(l) > 0
}

// Compose applies spec §3.5: base Recipe (Size, else item), plus Option
// deltas summed per Ingredient, clamped at zero, each line costed.
func (b *Book) Compose(sel Selection) Composition {
	base, ok := b.baseLines(sel)
	if !ok {
		return Composition{Lines: []ComposedLine{}, Status: CostNone}
	}
	totals := map[uuid.UUID]float64{}
	var order []uuid.UUID
	add := func(lines []Line) {
		for _, l := range lines {
			if _, seen := totals[l.IngredientID]; !seen {
				order = append(order, l.IngredientID)
			}
			totals[l.IngredientID] += l.StockQty
		}
	}
	add(base)
	for _, id := range sel.ModifierOptionIDs {
		add(b.Options[id])
	}

	out := Composition{Lines: make([]ComposedLine, 0, len(order)), Status: CostComplete}
	total := 0.0
	for _, id := range order {
		qty := Round3(totals[id])
		if qty <= 0 {
			continue
		}
		// Recording a sale must never fail on out-of-range recipe data
		// (spec §4.4): clamp to what the columns hold and flag the record.
		clamped := qty > MaxStockQuantity
		if clamped {
			qty = MaxStockQuantity
		}
		ing, ok := b.Ingredients[id]
		if !ok {
			continue
		}
		line := ComposedLine{IngredientID: id, Name: ing.Name, Kind: ing.Kind, StockUnit: ing.StockUnit, StockQty: qty}
		costed := false
		if c, ok := b.UnitCost(id); ok && !clamped {
			if lineCost := Round2(qty * c); lineCost <= MaxLineCostVND {
				unit := c
				line.UnitCost, line.LineCost = &unit, &lineCost
				total += lineCost
				costed = true
			}
		}
		if !costed {
			out.Status = CostPartial
		}
		out.Lines = append(out.Lines, line)
	}
	out.TotalVND = Round2(total)
	return out
}

// Delta is the cost one Modifier Option's Recipe adds or removes on its own.
type Delta struct {
	HasRecipe bool
	CostVND   float64
	Complete  bool
}

// OptionDelta costs an Option's delta lines without a base Recipe.
func (b *Book) OptionDelta(id uuid.UUID) Delta {
	lines, ok := b.Options[id]
	if !ok || len(lines) == 0 {
		return Delta{Complete: true}
	}
	d := Delta{HasRecipe: true, Complete: true}
	for _, l := range lines {
		c, ok := b.UnitCost(l.IngredientID)
		if !ok {
			d.Complete = false
			continue
		}
		d.CostVND += l.StockQty * c
	}
	d.CostVND = Round2(d.CostVND)
	return d
}
