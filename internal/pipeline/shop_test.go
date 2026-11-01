package pipeline

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
)

// loadCatalog is the catalog Shop labels, as the occasions directory's
// catalog.json loads.
func loadCatalog(t *testing.T) []domain.Product {
	t.Helper()
	loaded, err := domain.Load(filepath.Join(repoRoot(t), "testdata", "occasions"))
	if err != nil {
		t.Fatalf("load testdata/occasions: %v", err)
	}
	return loaded.Products
}

// wantCatalogLabels is every catalog product's label, read off the
// catalog's own fidelity field and spelled the domain's way: the file's
// short forms "exact" and "approximate" decode to the full labels, and
// the seven photographable products are exact because the task
// composites their photograph, the two lip colours approximate because
// makeup-vto paints a colour and never pastes the picture on.
var wantCatalogLabels = map[string]domain.Fidelity{
	"gold-jhumkas":      domain.ExactProduct,
	"gold-chain":        domain.ExactProduct,
	"top-festive":       domain.ExactProduct,
	"shirt-open-collar": domain.ExactProduct,
	"shirt-buttoned":    domain.ExactProduct,
	"shirt-dark-plain":  domain.ExactProduct,
	"lip-red":           domain.ApproximateMatch,
	"lip-nude":          domain.ApproximateMatch,
}

// wantSimCategories is the template-only category each photographing
// fix's suggestion is listed under, pinned here independently of the
// table it must equal.
var wantSimCategories = map[string]string{
	"hair-transfer": "hair",
	"beard-style":   "beard",
	"ai-smile":      "expression",
}

// ownJhumkas is a product the user brought - the intake record's shape:
// an id of its own, a name and category, origin user.
var ownJhumkas = &domain.Product{
	ID:       "b20d0c70000000000000000000000001",
	Category: "earrings",
	Name:     "Her own jhumkas",
	Origin:   domain.OriginUser,
}

// TestEveryFixShopsALabelledList is the exhaustive labelling. Over every
// fix on all five occasions - twenty-eight menus entries in all - the
// shop list carries the products the fix's own data names, every product
// carries one of the three labels, and each label is the one its rule
// gives it: a catalog product's label is the catalog's fidelity field, a
// photographing fix's suggestion is the visual simulation.
func TestEveryFixShopsALabelledList(t *testing.T) {
	occasions := loadOccasions(t)
	catalog := loadCatalog(t)
	labelled := map[string]domain.Fidelity{}
	fixes := 0

	for _, occ := range occasions {
		for i := range occ.Fixes {
			fix := &occ.Fixes[i]
			fixes++
			products, err := Shop(occ, fix, nil, catalog, nil)
			if err != nil {
				t.Fatalf("%s: %v", fix.ID, err)
			}
			if len(products) == 0 {
				t.Fatalf("%s: the shop list is empty", fix.ID)
			}
			for _, p := range products {
				if !p.Fidelity.Valid() {
					t.Fatalf("%s: product %q carries label %q, which is none of the three", fix.ID, p.ID, p.Fidelity)
				}
			}
			if len(fix.Products) > 0 {
				// One entry per product the fix names, in the fix's own
				// order, each the catalog's record under the catalog's
				// own label.
				if len(products) != len(fix.Products) {
					t.Fatalf("%s: %d entries for %d named products", fix.ID, len(products), len(fix.Products))
				}
				for j, id := range fix.Products {
					record, ok := catalogEntry(catalog, id)
					if !ok {
						t.Fatalf("%s: product %q is not in the catalog", fix.ID, id)
					}
					// The entry is the catalog's shop face: id, category,
					// name, the catalog's label, origin catalog - not the
					// shooting notes.
					want := domain.Product{
						ID:       record.ID,
						Category: record.Category,
						Name:     record.Name,
						Fidelity: record.Fidelity,
						Origin:   domain.OriginCatalog,
					}
					if products[j] != want {
						t.Fatalf("%s: entry %d is %+v, want the catalog's %+v", fix.ID, j, products[j], want)
					}
					labelled[id] = products[j].Fidelity
				}
				continue
			}
			// Nothing to photograph: one suggestion, the fix itself as
			// the entry, under the basket's template-only category.
			if len(products) != 1 {
				t.Fatalf("%s: %d entries, want the one suggestion", fix.ID, len(products))
			}
			sim := products[0]
			category, ok := wantSimCategories[fix.YouCam.Task]
			if !ok {
				t.Fatalf("%s: task %q photographs nothing and names no template-only category", fix.ID, fix.YouCam.Task)
			}
			if sim.ID != fix.ID || sim.Category != category || sim.Fidelity != domain.VisualSimulation || sim.Origin != domain.OriginCatalog {
				t.Fatalf("%s: the suggestion is %+v, want the fix under %q as a visual simulation", fix.ID, sim, category)
			}
			// The suggestion's name: the fix's display name from the
			// occasions data when one is recorded, else the fix's own
			// changes wording - the fallback that keeps an unworded fix
			// saying what it has always said.
			wantName := fix.Changes
			if fix.Display != nil && fix.Display.Name != "" {
				wantName = fix.Display.Name
			}
			if sim.Name != wantName {
				t.Fatalf("%s: the suggestion's name is %q, want %q", fix.ID, sim.Name, wantName)
			}
			offered := false
			for _, offeredByID := range occ.Basket.TemplateOnly {
				if offeredByID == category {
					offered = true
				}
			}
			if !offered {
				t.Fatalf("%s: category %q is not on the basket", fix.ID, category)
			}
		}
	}

	if fixes != 28 {
		t.Fatalf("swept %d fixes, want the menus' 28", fixes)
	}
	if len(labelled) != len(wantCatalogLabels) {
		t.Fatalf("labelled %d catalog products, want all %d", len(labelled), len(wantCatalogLabels))
	}
	for id, want := range wantCatalogLabels {
		if got := labelled[id]; got != want {
			t.Fatalf("product %q is labelled %q, want %q", id, got, want)
		}
	}
}

// TestTheLabelRules exercises each label's rule by name: the exact
// product for the user's own item and for the recorded catalog product,
// the approximate match for the lip the task paints, the visual
// simulation for the fix with nothing to photograph - and the wire
// spellings the app's reader demands.
func TestTheLabelRules(t *testing.T) {
	occasions := loadOccasions(t)
	catalog := loadCatalog(t)
	family := occasions["family-function"]
	jhumkas := measureFix(t, family, "family-gold-jhumkas")
	lip := measureFix(t, family, "family-red-lip")
	smile := measureFix(t, family, "family-closed-smile")

	// Exact product, the user's own item: the earring task composites a
	// product photo, so her own jhumkas are listed, first, exact, hers.
	own, err := Shop(family, jhumkas, ownJhumkas, catalog, nil)
	if err != nil {
		t.Fatalf("the own item: %v", err)
	}
	if len(own) != 2 {
		t.Fatalf("the own item: %d entries, want hers and the catalog's", len(own))
	}
	if own[0].ID != ownJhumkas.ID || own[0].Fidelity != domain.ExactProduct || own[0].Origin != domain.OriginUser {
		t.Fatalf("the own item: first entry %+v, want her jhumkas as an exact product of the user", own[0])
	}
	if own[1].ID != "gold-jhumkas" || own[1].Fidelity != domain.ExactProduct || own[1].Origin != domain.OriginCatalog {
		t.Fatalf("the own item: second entry %+v, want the catalog jhumkas as an exact product of the catalog", own[1])
	}

	// Exact product, the recorded catalog product: nothing brought, and
	// the list is the occasion's own jhumkas, exact because the task
	// composites that photograph.
	basketOnly, err := Shop(family, jhumkas, nil, catalog, nil)
	if err != nil {
		t.Fatalf("the catalog product: %v", err)
	}
	if len(basketOnly) != 1 || basketOnly[0].ID != "gold-jhumkas" || basketOnly[0].Fidelity != domain.ExactProduct {
		t.Fatalf("the catalog product: %+v, want gold-jhumkas exact", basketOnly)
	}

	// Approximate match: the lip fix names the catalog's red, recorded
	// approximate because the task paints a colour and never pastes the
	// picture on.
	lipList, err := Shop(family, lip, nil, catalog, nil)
	if err != nil {
		t.Fatalf("the lip: %v", err)
	}
	if len(lipList) != 1 || lipList[0].ID != "lip-red" || lipList[0].Fidelity != domain.ApproximateMatch || lipList[0].Origin != domain.OriginCatalog {
		t.Fatalf("the lip: %+v, want lip-red approximate match", lipList)
	}

	// Visual simulation: the smile names no product, the render shows
	// the effect, and the entry is the suggestion itself - the fix id,
	// the basket's expression category, no catalog product behind it.
	simList, err := Shop(family, smile, nil, catalog, nil)
	if err != nil {
		t.Fatalf("the smile: %v", err)
	}
	if len(simList) != 1 {
		t.Fatalf("the smile: %d entries, want the one suggestion", len(simList))
	}
	sim := simList[0]
	if sim.ID != "family-closed-smile" || sim.Category != "expression" || sim.Fidelity != domain.VisualSimulation || sim.Origin != domain.OriginCatalog {
		t.Fatalf("the smile: %+v, want the fix under expression as a visual simulation", sim)
	}
	// The suggestion's name is the fix's display name from the occasions
	// data - "Closed-mouth smile", the shop fixture's own English
	// (testdata/api/shop-simulation.json), now derived from the fix's
	// display entry instead of hand-named in the fixture alone.
	if sim.Name != "Closed-mouth smile" {
		t.Fatalf("the smile: name %q, want the display table's %q", sim.Name, "Closed-mouth smile")
	}

	// A fix with no display entry keeps its changes wording: the
	// fallback that preserves every name the pipeline has always said.
	date := occasions["date"]
	dateSmile := measureFix(t, date, "date-closed-smile")
	dateSim, err := Shop(date, dateSmile, nil, catalog, nil)
	if err != nil {
		t.Fatalf("the date smile: %v", err)
	}
	if len(dateSim) != 1 || dateSim[0].Name != dateSmile.Changes {
		t.Fatalf("the date smile: %+v, want the changes wording %q", dateSim, dateSmile.Changes)
	}

	// The spellings on the wire are the domain labels, exactly, and a
	// shop entry carries nothing of the catalog's interior.
	for _, entry := range []domain.Product{own[0], lipList[0], sim} {
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal %q: %v", entry.ID, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode %q: %v", entry.ID, err)
		}
		if decoded["fidelity"] != string(entry.Fidelity) {
			t.Fatalf("%q: fidelity %v on the wire, want exactly %q", entry.ID, decoded["fidelity"], entry.Fidelity)
		}
		for _, interior := range []string{"youcam_role", "photograph"} {
			if _, ok := decoded[interior]; ok {
				t.Fatalf("%q: the shop entry carries the catalog's %s", entry.ID, interior)
			}
		}
	}

	// A catalog id the user brought is the catalog's entry, once, under
	// the catalog's own label - the catalog's word covers what the task
	// does with that photograph.
	broughtCatalog := &domain.Product{ID: "gold-jhumkas", Category: "earrings", Name: "Gold jhumkas", Origin: domain.OriginUser}
	dedup, err := Shop(family, jhumkas, broughtCatalog, catalog, nil)
	if err != nil {
		t.Fatalf("the brought catalog id: %v", err)
	}
	if len(dedup) != 1 {
		t.Fatalf("the brought catalog id: %d entries, want the one catalog entry", len(dedup))
	}
	if dedup[0].Origin != domain.OriginCatalog || dedup[0].Fidelity != domain.ExactProduct || dedup[0].Name != "Gold jhumkas" {
		t.Fatalf("the brought catalog id: %+v, want the catalog's record", dedup[0])
	}

	// The user's own item is listed where the render composites one. The
	// lip task paints a colour and composites nothing, so her jhumkas
	// are not on the lip's list - the list is the fix's, not the look's
	// bag.
	mismatch, err := Shop(family, lip, ownJhumkas, catalog, nil)
	if err != nil {
		t.Fatalf("the lip with an own item: %v", err)
	}
	if len(mismatch) != 1 || mismatch[0].ID != "lip-red" {
		t.Fatalf("the lip with an own item: %+v, want lip-red alone", mismatch)
	}
}

// TestTheShopListIsShorterWhenThereIsNothingToShop is the empty case,
// honest: no fix picked, or no product for the fix and no category the
// basket offers, and the list is empty - never invented. The refusals
// name the wiring bugs the data cannot produce.
func TestTheShopListIsShorterWhenThereIsNothingToShop(t *testing.T) {
	occasions := loadOccasions(t)
	catalog := loadCatalog(t)
	family := occasions["family-function"]

	// A look that picked no fix shops nothing.
	none, err := Shop(family, nil, ownJhumkas, catalog, nil)
	if err != nil {
		t.Fatalf("no fix: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("no fix: %d entries, want none", len(none))
	}

	// A fix with nothing to photograph whose category the basket does
	// not offer: the list is simply shorter.
	withheld := &domain.Occasion{
		ID:     "family-function",
		Basket: domain.Basket{TemplateOnly: []string{"beard"}},
	}
	hairless := &domain.Fix{ID: "family-tie-hair", Changes: "Ties the hair back.", YouCam: domain.YouCam{Task: "hair-transfer"}}
	short, err := Shop(withheld, hairless, nil, catalog, nil)
	if err != nil {
		t.Fatalf("a category the basket withholds: %v", err)
	}
	if len(short) != 0 {
		t.Fatalf("a category the basket withholds: %+v, want an empty list", short)
	}

	// The positive control: the same fix under a basket that offers the
	// category is the one suggestion.
	offering := &domain.Occasion{
		ID:     "family-function",
		Basket: domain.Basket{TemplateOnly: []string{"hair", "beard"}},
	}
	shopped, err := Shop(offering, hairless, nil, catalog, nil)
	if err != nil {
		t.Fatalf("a category the basket offers: %v", err)
	}
	if len(shopped) != 1 || shopped[0].ID != "family-tie-hair" || shopped[0].Category != "hair" || shopped[0].Fidelity != domain.VisualSimulation {
		t.Fatalf("a category the basket offers: %+v, want the hair suggestion", shopped)
	}

	// Refusals: no occasion; a fix naming a product the catalog
	// argument does not hold; an own item with no id where one would be
	// listed.
	if _, err := Shop(nil, hairless, nil, catalog, nil); err == nil {
		t.Fatal("no occasion: want a refusal")
	}
	stray := &domain.Fix{ID: "stray", YouCam: domain.YouCam{Task: "2d-vto/earring"}, Products: []string{"gold-jhumkas"}}
	if _, err := Shop(family, stray, nil, nil, nil); err == nil {
		t.Fatal("a product the catalog argument lacks: want a refusal")
	}
	idless := &domain.Product{Category: "earrings", Name: "Her own jhumkas", Origin: domain.OriginUser}
	if _, err := Shop(family, measureFix(t, family, "family-gold-jhumkas"), idless, catalog, nil); err == nil {
		t.Fatal("an own item with no id: want a refusal")
	}
}

// TestKeptProductsRankFirst is the ranking. With history on, a product a
// kept fix names ranks first - the fix mapping to products through its
// own products field - and with history off, or a kept fix that names
// nothing here, the base order stands: her own item, then the fix's
// catalog products in the fix's own order.
func TestKeptProductsRankFirst(t *testing.T) {
	occasions := loadOccasions(t)
	catalog := loadCatalog(t)
	family := occasions["family-function"]
	jhumkas := measureFix(t, family, "family-gold-jhumkas")

	assertOrder := func(what string, products []domain.Product, want ...string) {
		t.Helper()
		if len(products) != len(want) {
			t.Fatalf("%s: %d entries, want %d", what, len(products), len(want))
		}
		for i, id := range want {
			if products[i].ID != id {
				t.Fatalf("%s: entry %d is %q, want %q", what, i, products[i].ID, id)
			}
		}
	}

	// History off: her own item, then the catalog's.
	off, err := Shop(family, jhumkas, ownJhumkas, catalog, nil)
	if err != nil {
		t.Fatalf("history off: %v", err)
	}
	assertOrder("history off", off, ownJhumkas.ID, "gold-jhumkas")

	// History on, the second visit's own memory: the kept jhumka fix
	// maps to gold-jhumkas, and the jhumkas rank first.
	on, err := Shop(family, jhumkas, ownJhumkas, catalog, storyRecall())
	if err != nil {
		t.Fatalf("history on: %v", err)
	}
	assertOrder("history on", on, "gold-jhumkas", ownJhumkas.ID)

	// History on, but the kept fix names nothing in this list: the base
	// order stands.
	other, err := Shop(family, jhumkas, ownJhumkas, catalog, &Recall{Kept: []string{"family-red-lip"}})
	if err != nil {
		t.Fatalf("a kept fix naming nothing here: %v", err)
	}
	assertOrder("a kept fix naming nothing here", other, ownJhumkas.ID, "gold-jhumkas")

	// A kept fix with no products - a template fix - names nothing: the
	// base order stands.
	template, err := Shop(family, jhumkas, ownJhumkas, catalog, &Recall{Kept: []string{"family-tie-hair"}})
	if err != nil {
		t.Fatalf("a kept fix with no products: %v", err)
	}
	assertOrder("a kept fix with no products", template, ownJhumkas.ID, "gold-jhumkas")

	// A stale kept id that names no fix here is a no-op.
	stale, err := Shop(family, jhumkas, ownJhumkas, catalog, &Recall{Kept: []string{"family-gone"}})
	if err != nil {
		t.Fatalf("a stale kept id: %v", err)
	}
	assertOrder("a stale kept id", stale, ownJhumkas.ID, "gold-jhumkas")

	// An empty recall is history off, the pick's own reading of it.
	empty, err := Shop(family, jhumkas, ownJhumkas, catalog, &Recall{})
	if err != nil {
		t.Fatalf("an empty recall: %v", err)
	}
	assertOrder("an empty recall", empty, ownJhumkas.ID, "gold-jhumkas")

	// Nothing brought and history on: the single entry does not move,
	// whatever the memory says.
	single, err := Shop(family, jhumkas, nil, catalog, storyRecall())
	if err != nil {
		t.Fatalf("one entry: %v", err)
	}
	assertOrder("one entry", single, "gold-jhumkas")
}
