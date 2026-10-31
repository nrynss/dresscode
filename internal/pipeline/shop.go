// Shop is this file: the products for the chosen fix, each carrying one
// of the three fidelity labels, in the order they are offered. It reads
// no model and no provider: every input is data already in hand - the
// occasion, the fix the pick chose, the product the user brought, the
// catalog, and the account's recall when history is on - so the function
// takes no client, nothing can dial out of it, and the stage costs
// nothing. A look whose render was refused still shops; the suggestion
// stands and the products stand with it.

package pipeline

import (
	"errors"
	"fmt"

	"github.com/nrynss/dresscode/internal/domain"
)

// simulationCategories maps the YouCam task of a fix with nothing to
// photograph onto the template-only category its shop suggestion is
// listed under (testdata/api/shop-simulation.json: the smile's category
// is the basket's own "expression"). The basket names the categories;
// this table only says which one a task's suggestion is, and Shop
// checks the answer against the basket before it lists the entry.
var simulationCategories = map[string]string{
	"hair-transfer": "hair",
	"beard-style":   "beard",
	"ai-smile":      "expression",
}

// Shop builds the shop list for one look: the products for the chosen
// fix, from the user's own items and the occasion's basket, each labelled
// exact product, approximate match or visual simulation, products the
// user kept before first when history is on. Every product carries a
// label; the empty case is a shorter list, never an invented product.
//
// The label rules - the domain's honesty vocabulary (domain.Fidelity),
// one rule per label:
//
//   - Exact product (domain.ExactProduct): the render shows this very
//     piece. Either it is the user's own item - a product the user
//     brought that is no catalog entry, listed only for a fix whose
//     YouCam task composites product photos, the same gate the run
//     resolves the product's bytes under, so the render wears the bytes
//     the user supplied - or it is the recorded catalog product for the
//     named fix, whose photograph is what the task composites
//     (docs/occasions.md: "The jhumkas are an exact product because the
//     task composites that photograph").
//
//   - Approximate match (domain.ApproximateMatch): a catalog product
//     close to the named fix but not the exact item - the catalog
//     records it "approximate" because the task paints the effect and
//     never pastes this picture on (the red lip: "the catalog photo is
//     the colour we mean, not an image it pastes on").
//
//   - Visual simulation (domain.VisualSimulation): no catalog product
//     exists for the fix - hair, beard and smile render from a template,
//     with nothing to photograph. The render shows the effect on the
//     user's own photo, and the shop entry is the shopping suggestion
//     itself: the fix id as the id, no picture, and the basket's own
//     template-only category for the fix's task.
//
// The labels are data-driven wherever the data can express them: a
// catalog product's label is the catalog's own fidelity field, decoded by
// domain.Fidelity, whatever brought it. The catalog records that field
// from the task's own relationship to the product, so a product the user
// brought by catalog id is the same entry under the same label - one
// entry per id, listed where the fix names it, the catalog's word
// covering what the task does with that photograph. Only an item outside
// the catalog is labelled by this file's own rule, and its rule is the
// first clause: the user brought this very item, origin user. A
// simulation's name is the fix's own changes wording, copied verbatim;
// no occasions field carries a display name for a fix.
//
// The order. When history is on - the recall names kept fixes - a
// product a kept fix names ranks first. The fix maps to products through
// its own products field, the same list the loader checks the catalog
// and the basket against, so the ranking reads the data's own mapping
// and nothing else. The rest hold the base order: the user's own item,
// then the fix's catalog products in the fix's own order. History off (a
// nil or empty recall) reorders nothing. A kept id that names no fix on
// this occasion is a no-op, the tolerance pick grants a stale dismissal.
//
// The empty cases. A look that picked no fix shops nothing. A fix the
// catalog holds no product for, whose task's template-only category this
// occasion's basket does not offer, shops nothing either: the basket is
// the data's own word for what this occasion's shop carries, and the
// list is simply shorter. So does a fix whose task no template-only
// category is recorded for at all - the suggestion has no category to
// wear, on this basket or any other. Nothing is invented to fill it.
//
// Refused, not shortened: an ask without an occasion, a brought item
// without an id on a fix that would list it, and a fix naming a product
// the catalog argument does not hold. The loader checks every fix's
// products against the catalog and every basket product's category
// against the basket, so the first and the last can only come from two
// loads mixed - a wiring bug, named loudly. The basket's template_only
// list is the one basket rule the loader does not tie to the fixes, and
// a missing category there omits the entry rather than failing a look
// that has already rendered.
func Shop(occ *domain.Occasion, fix *domain.Fix, brought *domain.Product, catalog []domain.Product, recall *Recall) ([]domain.Product, error) {
	if occ == nil {
		return nil, errors.New("shop: no occasion")
	}
	if fix == nil {
		// No fix was picked: the look stands on its suggestions, and
		// there is nothing to shop for. The empty list is the answer,
		// not an error.
		return []domain.Product{}, nil
	}

	entries := make([]domain.Product, 0, len(fix.Products)+1)

	// The user's own item, listed only where the render composites a
	// product photo. A catalog id the user brought is not their own
	// entry: it collapses into the catalog's, below, under the catalog's
	// own label.
	if needProduct(fix) && brought != nil {
		if brought.ID == "" {
			return nil, errors.New("shop: the brought product has no id")
		}
		if _, ok := catalogEntry(catalog, brought.ID); !ok {
			entries = append(entries, domain.Product{
				ID:       brought.ID,
				Category: brought.Category,
				Name:     brought.Name,
				Fidelity: domain.ExactProduct,
				Origin:   domain.OriginUser,
			})
		}
	}

	// The fix's catalog products, each labelled from the catalog's own
	// field. The shooting notes (youcam_role, photograph) are the
	// catalog's interior data and do not travel with a shop entry.
	for _, id := range fix.Products {
		p, ok := catalogEntry(catalog, id)
		if !ok {
			return nil, fmt.Errorf("shop: fix %q names product %q, which the catalog does not hold", fix.ID, id)
		}
		entries = append(entries, domain.Product{
			ID:       p.ID,
			Category: p.Category,
			Name:     p.Name,
			Fidelity: p.Fidelity,
			Origin:   domain.OriginCatalog,
		})
	}

	// Nothing to photograph: the suggestion itself is the entry, when
	// this occasion's basket offers a category for it.
	if len(fix.Products) == 0 {
		if category, ok := simulationCategory(occ, fix); ok {
			entries = append(entries, domain.Product{
				ID:       fix.ID,
				Category: category,
				Name:     fix.Changes,
				Fidelity: domain.VisualSimulation,
				Origin:   domain.OriginCatalog,
			})
		}
	}

	return rankKeptFirst(entries, occ, recall), nil
}

// catalogEntry finds one catalog product by id. The catalog is the
// occasions directory's catalog.json as domain.Load read it.
func catalogEntry(catalog []domain.Product, id string) (domain.Product, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return domain.Product{}, false
}

// simulationCategory names the basket's template-only category a fix's
// shop suggestion is listed under. The table answers for the three tasks
// that photograph nothing; the basket has the final word - a category
// the occasion does not offer is no entry, and the list is simply
// shorter.
func simulationCategory(occ *domain.Occasion, fix *domain.Fix) (string, bool) {
	category, ok := simulationCategories[fix.YouCam.Task]
	if !ok {
		return "", false
	}
	for _, offered := range occ.Basket.TemplateOnly {
		if offered == category {
			return category, true
		}
	}
	return "", false
}

// rankKeptFirst moves the products a kept fix names to the front when
// history is on. The mapping from a kept fix to products is the fix's
// own products field, so the ranking reads the data's own mapping and
// nothing else; a kept id that names no fix here is a no-op. A nil or
// empty recall is history off, and reorders nothing. The order is
// otherwise the entries' own: the user's own item first, then the fix's
// catalog products in the fix's own order.
func rankKeptFirst(entries []domain.Product, occ *domain.Occasion, recall *Recall) []domain.Product {
	if recall.empty() || len(entries) < 2 {
		return entries
	}
	keptFixes := make(map[string]struct{}, len(recall.Kept))
	for _, id := range recall.Kept {
		keptFixes[id] = struct{}{}
	}
	kept := map[string]struct{}{}
	for i := range occ.Fixes {
		fix := &occ.Fixes[i]
		if _, ok := keptFixes[fix.ID]; !ok {
			continue
		}
		for _, id := range fix.Products {
			kept[id] = struct{}{}
		}
	}
	if len(kept) == 0 {
		return entries
	}
	ordered := make([]domain.Product, 0, len(entries))
	for _, p := range entries {
		if _, ok := kept[p.ID]; ok {
			ordered = append(ordered, p)
		}
	}
	for _, p := range entries {
		if _, ok := kept[p.ID]; !ok {
			ordered = append(ordered, p)
		}
	}
	return ordered
}
