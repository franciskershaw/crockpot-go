package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	noteRecipeSkipped       = "recipe-skipped"
	noteItemSkipped         = "item-skipped"
	noteDuplicateIngredient = "duplicate-ingredient"
	noteUnitNulled          = "unit-nulled"
	noteUnitBlank           = "unit-blank"
	noteAllowedSetDropped   = "allowed-set-dropped"
	noteAllowedUnitWidened  = "allowed-unit-widened"
	noteAllowedUnitSkipped  = "allowed-unit-addition-skipped"
	noteCreatorUnresolved   = "creator-unresolved"
	noteCreatedAtFallback   = "created-at-fallback"
	noteCategoryLinkDropped = "category-link-dropped"
	noteDuplicateCategory   = "duplicate-category"
	noteGoogleSubPending    = "google-sub-pending"
)

type transformNote struct {
	Kind   string
	Entity string
	Detail string
}

type resolvedCreator struct {
	ID   uuid.UUID
	Name string
}

type userRow struct {
	SourceID        oid
	ID              uuid.UUID
	Email           string
	GoogleID        string
	Name            *string
	Image           *string
	Role            string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type itemRow struct {
	SourceID       oid
	ID             uuid.UUID
	Name           string
	CategoryID     uuid.UUID
	AllowedUnitIDs []uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ingredientRow struct {
	ItemID   uuid.UUID
	UnitID   *uuid.UUID
	Quantity float64
	Position int
}

type recipeRow struct {
	ID            uuid.UUID
	Name          string
	TimeInMinutes int
	Serves        int
	ImageURL      *string
	ImageFilename *string
	Instructions  []string
	Notes         []string
	Approved      bool
	CreatedByID   *uuid.UUID
	CreatedByName *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Ingredients   []ingredientRow
	CategoryIDs   []uuid.UUID
}

type itemDeps struct {
	categories    *refResolver
	units         *refResolver
	unitIDByName  map[string]uuid.UUID
	ignoreAllowed bool
}

type recipeDeps struct {
	itemUUID    map[oid]uuid.UUID
	itemAllowed map[oid][]uuid.UUID
	categories  *refResolver
	units       *refResolver
	creators    map[oid]resolvedCreator
}

type transformInput struct {
	src             *source
	itemCategories  *refResolver
	units           *refResolver
	recipeCategs    *refResolver
	unitIDByName    map[string]uuid.UUID
	accountSubs     map[oid]string
	ignoreAllowed   bool
	allowMissingSub bool
}

type transformResult struct {
	Users   []userRow
	Items   []itemRow
	Recipes []recipeRow
	Notes   []transformNote

	// source rows dropped because their whole recipe was skipped - tracked
	// so reconcile() can derive "skipped" independently of destination counts.
	SkippedIngredients   int
	SkippedCategoryLinks int
}

func (r *transformResult) skipped() bool {
	for _, n := range r.Notes {
		if n.Kind == noteRecipeSkipped || n.Kind == noteItemSkipped {
			return true
		}
	}
	return false
}

func transform(in transformInput) (*transformResult, error) {
	users, userNotes, err := buildUsers(in.src.Users, in.accountSubs, in.allowMissingSub)
	if err != nil {
		return nil, err
	}

	items, itemNotes := buildItems(in.src.Items, itemDeps{
		categories:    in.itemCategories,
		units:         in.units,
		unitIDByName:  in.unitIDByName,
		ignoreAllowed: in.ignoreAllowed,
	})

	itemUUID := make(map[oid]uuid.UUID, len(items))
	itemAllowed := make(map[oid][]uuid.UUID, len(items))
	for _, it := range items {
		itemUUID[it.SourceID] = it.ID
		itemAllowed[it.SourceID] = it.AllowedUnitIDs
	}

	recipes, recipeNotes, skips := buildRecipes(in.src.Recipes, recipeDeps{
		itemUUID:    itemUUID,
		itemAllowed: itemAllowed,
		categories:  in.recipeCategs,
		units:       in.units,
		creators:    creatorLookup(users),
	})

	notes := make([]transformNote, 0, len(userNotes)+len(itemNotes)+len(recipeNotes))
	notes = append(notes, userNotes...)
	notes = append(notes, itemNotes...)
	notes = append(notes, recipeNotes...)
	return &transformResult{
		Users: users, Items: items, Recipes: recipes, Notes: notes,
		SkippedIngredients:   skips.ingredients,
		SkippedCategoryLinks: skips.categoryLinks,
	}, nil
}

func buildUsers(users []mongoUser, subs map[oid]string, allowMissingSub bool) ([]userRow, []transformNote, error) {
	byID := make(map[oid]mongoUser, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}

	var rows []userRow
	var notes []transformNote

	for _, adminID := range realAdmins {
		src, ok := byID[adminID]
		if !ok {
			return nil, nil, fmt.Errorf("admin user %s is absent from the User export", adminID)
		}
		gid := subs[src.ID]
		if gid == "" {
			if !allowMissingSub {
				return nil, nil, fmt.Errorf(
					"user %s (%s) has no google account in Account.json; pass --allow-missing-google-sub to insert a placeholder",
					src.ID, src.Email)
			}
			gid = "pending:" + src.Email
			notes = append(notes, transformNote{Kind: noteGoogleSubPending, Entity: src.Email,
				Detail: "inserted google_id " + gid + "; fix with an UPDATE once the real sub is known"})
		}
		verified := src.CreatedAt.Time
		if src.EmailVerified != nil && !src.EmailVerified.IsZero() {
			verified = src.EmailVerified.Time
		}
		rows = append(rows, userRow{
			SourceID:        src.ID,
			ID:              objectIDToUUID(src.ID),
			Email:           src.Email,
			GoogleID:        gid,
			Name:            src.Name,
			Image:           src.Image,
			Role:            "ADMIN",
			EmailVerifiedAt: &verified,
			CreatedAt:       src.CreatedAt.Time,
			UpdatedAt:       src.UpdatedAt.Time,
		})
	}

	seedTime := objectIDTime(syntheticCrockpotUser.SourceID)
	if seedTime.IsZero() {
		seedTime = time.Now().UTC()
	}
	name := syntheticCrockpotUser.Name
	rows = append(rows, userRow{
		SourceID:        syntheticCrockpotUser.SourceID,
		ID:              objectIDToUUID(syntheticCrockpotUser.SourceID),
		Email:           syntheticCrockpotUser.Email,
		GoogleID:        syntheticCrockpotUser.GoogleID,
		Name:            &name,
		Role:            "ADMIN",
		EmailVerifiedAt: &seedTime,
		CreatedAt:       seedTime,
		UpdatedAt:       seedTime,
	})

	return rows, notes, nil
}

func creatorLookup(users []userRow) map[oid]resolvedCreator {
	m := make(map[oid]resolvedCreator, len(users))
	for _, u := range users {
		name := ""
		if u.Name != nil {
			name = *u.Name
		}
		m[u.SourceID] = resolvedCreator{ID: u.ID, Name: name}
	}
	return m
}

func buildItems(items []mongoItem, d itemDeps) ([]itemRow, []transformNote) {
	var rows []itemRow
	var notes []transformNote

	for _, mi := range items {
		catID, ok := d.categories.byMongoID[mi.CategoryID]
		if !ok {
			notes = append(notes, transformNote{Kind: noteItemSkipped, Entity: mi.Name,
				Detail: "item category did not resolve"})
			continue
		}

		var allowed []uuid.UUID
		if !d.ignoreAllowed {
			allowed = resolveAllowedUnits(mi, d.units, &notes)
		}
		allowed = applyUnitAdditions(mi.Name, allowed, d.unitIDByName, &notes)

		rows = append(rows, itemRow{
			SourceID:       mi.ID,
			ID:             objectIDToUUID(mi.ID),
			Name:           mi.Name,
			CategoryID:     catID,
			AllowedUnitIDs: allowed,
			CreatedAt:      fallbackTime(mi.CreatedAt.Time, mi.UpdatedAt.Time),
			UpdatedAt:      fallbackTime(mi.UpdatedAt.Time, mi.CreatedAt.Time),
		})
	}
	return rows, notes
}

func resolveAllowedUnits(mi mongoItem, units *refResolver, notes *[]transformNote) []uuid.UUID {
	if len(mi.AllowedUnitIDs) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(mi.AllowedUnitIDs))
	for _, u := range mi.AllowedUnitIDs {
		id, ok := units.byMongoID[u]
		if !ok {
			*notes = append(*notes, transformNote{Kind: noteAllowedSetDropped, Entity: mi.Name,
				Detail: "an allowed-unit entry did not resolve; whole set dropped (now unconstrained)"})
			return nil
		}
		out = append(out, id)
	}
	return out
}

func applyUnitAdditions(name string, allowed []uuid.UUID, unitIDByName map[string]uuid.UUID, notes *[]transformNote) []uuid.UUID {
	adds, ok := allowedUnitAdditions[name]
	if !ok {
		return allowed
	}
	if len(allowed) == 0 {
		*notes = append(*notes, transformNote{Kind: noteAllowedUnitSkipped, Entity: name,
			Detail: "item is unconstrained; additions skipped so it stays that way"})
		return allowed
	}
	for _, unitName := range adds {
		id, ok := unitIDByName[unitName]
		if !ok {
			continue
		}
		if !containsUUID(allowed, id) {
			allowed = append(allowed, id)
			*notes = append(*notes, transformNote{Kind: noteAllowedUnitWidened, Entity: name, Detail: "+" + unitName})
		}
	}
	return allowed
}

type recipeSkipTally struct {
	ingredients   int
	categoryLinks int
}

func buildRecipes(recipes []mongoRecipe, d recipeDeps) ([]recipeRow, []transformNote, recipeSkipTally) {
	var rows []recipeRow
	var notes []transformNote
	var skips recipeSkipTally

	for _, mr := range recipes {
		rr, rnotes, skip := transformRecipe(mr, d)
		notes = append(notes, rnotes...)
		if skip {
			skips.ingredients += len(mr.Ingredients)
			skips.categoryLinks += len(mr.CategoryIDs)
			continue
		}
		rows = append(rows, rr)
	}
	return rows, notes, skips
}

func transformRecipe(mr mongoRecipe, d recipeDeps) (recipeRow, []transformNote, bool) {
	var notes []transformNote

	createdByID, createdByName, cnote := resolveCreator(mr, d.creators)
	if cnote != nil {
		notes = append(notes, *cnote)
	}

	ings, inote, skip := resolveIngredients(mr, d)
	notes = append(notes, inote...)
	if skip {
		return recipeRow{}, notes, true
	}

	var catIDs []uuid.UUID
	seenCat := map[oid]bool{}
	for _, c := range mr.CategoryIDs {
		if seenCat[c] {
			notes = append(notes, transformNote{Kind: noteDuplicateCategory, Entity: mr.Name,
				Detail: "category " + string(c) + " listed twice; kept once"})
			continue
		}
		seenCat[c] = true
		id, ok := d.categories.byMongoID[c]
		if !ok {
			notes = append(notes, transformNote{Kind: noteCategoryLinkDropped, Entity: mr.Name,
				Detail: "category " + string(c) + " did not resolve"})
			continue
		}
		catIDs = append(catIDs, id)
	}

	created := mr.CreatedAt.Time
	updated := mr.UpdatedAt.Time
	if created.IsZero() {
		created = updated
		if created.IsZero() {
			created = time.Now().UTC()
		}
		notes = append(notes, transformNote{Kind: noteCreatedAtFallback, Entity: mr.Name})
	}
	if updated.IsZero() {
		updated = created
	}

	instructions := mr.Instructions
	if instructions == nil {
		instructions = []string{}
	}
	recNotes := mr.Notes
	if recNotes == nil {
		recNotes = []string{}
	}

	var imageURL, imageFilename *string
	if mr.Image != nil {
		imageURL = mr.Image.URL
		imageFilename = mr.Image.Filename
	}

	return recipeRow{
		ID:            objectIDToUUID(mr.ID),
		Name:          mr.Name,
		TimeInMinutes: int(mr.TimeInMinutes),
		Serves:        int(mr.Serves),
		ImageURL:      imageURL,
		ImageFilename: imageFilename,
		Instructions:  instructions,
		Notes:         recNotes,
		Approved:      mr.Approved,
		CreatedByID:   createdByID,
		CreatedByName: createdByName,
		CreatedAt:     created,
		UpdatedAt:     updated,
		Ingredients:   ings,
		CategoryIDs:   catIDs,
	}, notes, false
}

func resolveCreator(mr mongoRecipe, creators map[oid]resolvedCreator) (*uuid.UUID, *string, *transformNote) {
	if mr.CreatedByID == nil {
		return nil, nil, &transformNote{Kind: noteCreatorUnresolved, Entity: mr.Name, Detail: "no createdById"}
	}
	c, ok := creators[*mr.CreatedByID]
	if !ok {
		return nil, nil, &transformNote{Kind: noteCreatorUnresolved, Entity: mr.Name,
			Detail: "createdById " + string(*mr.CreatedByID) + " not among migrated users"}
	}
	id := c.ID
	name := c.Name
	return &id, &name, nil
}

func resolveIngredients(mr mongoRecipe, d recipeDeps) ([]ingredientRow, []transformNote, bool) {
	var notes []transformNote
	seen := map[oid]bool{}
	var ings []ingredientRow

	for _, mi := range mr.Ingredients {
		itemID, ok := d.itemUUID[mi.ItemID]
		if !ok {
			notes = append(notes, transformNote{Kind: noteRecipeSkipped, Entity: mr.Name,
				Detail: "ingredient item " + string(mi.ItemID) + " did not resolve"})
			return nil, notes, true
		}
		if seen[mi.ItemID] {
			notes = append(notes, transformNote{Kind: noteDuplicateIngredient, Entity: mr.Name,
				Detail: "duplicate item " + string(mi.ItemID) + "; kept first"})
			continue
		}
		seen[mi.ItemID] = true

		unitID, unote, skip := resolveIngredientUnit(mr, mi, d)
		if unote != nil {
			notes = append(notes, *unote)
		}
		if skip {
			return nil, notes, true
		}

		ings = append(ings, ingredientRow{
			ItemID:   itemID,
			UnitID:   unitID,
			Quantity: float64(mi.Quantity),
			Position: len(ings),
		})
	}
	return ings, notes, false
}

func resolveIngredientUnit(mr mongoRecipe, mi mongoIngredient, d recipeDeps) (*uuid.UUID, *transformNote, bool) {
	if mi.UnitID == nil {
		return nil, nil, false
	}
	uid, ok := d.units.byMongoID[*mi.UnitID]
	if !ok {
		if d.units.dropped[*mi.UnitID] {
			return nil, &transformNote{Kind: noteUnitBlank, Entity: mr.Name,
				Detail: "old blank-unit convention; stored as no unit"}, false
		}
		return nil, &transformNote{Kind: noteUnitNulled, Entity: mr.Name,
			Detail: "unit " + string(*mi.UnitID) + " did not resolve; nulled"}, false
	}
	allowed := d.itemAllowed[mi.ItemID]
	if len(allowed) > 0 && !containsUUID(allowed, uid) {
		return nil, &transformNote{Kind: noteRecipeSkipped, Entity: mr.Name,
			Detail: "unit not in item's allowed set"}, true
	}
	u := uid
	return &u, nil, false
}

func objectIDTime(o oid) time.Time {
	if len(o) < 8 {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(string(o[:8]), 16, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(secs, 0).UTC()
}

func fallbackTime(primary, secondary time.Time) time.Time {
	switch {
	case !primary.IsZero():
		return primary
	case !secondary.IsZero():
		return secondary
	default:
		return time.Now().UTC()
	}
}

func containsUUID(s []uuid.UUID, v uuid.UUID) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
