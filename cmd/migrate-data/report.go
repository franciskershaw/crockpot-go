package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type reconciliation struct {
	Entity    string
	Source    int
	Skipped   int // derived from notes / skip tallies, independent of Built
	Built     int // rows the transform produced
	Persisted int // rows in the DB after commit; -1 if not checked (dry run)
}

func reconcile(res *transformResult, src *source, persisted map[string]int) []reconciliation {
	counts := noteCounts(res)

	srcIngs, srcCats := 0, 0
	for _, r := range src.Recipes {
		srcIngs += len(r.Ingredients)
		srcCats += len(r.CategoryIDs)
	}
	builtIngs, builtCats := 0, 0
	for _, r := range res.Recipes {
		builtIngs += len(r.Ingredients)
		builtCats += len(r.CategoryIDs)
	}
	inDB := func(k string) int {
		if persisted == nil {
			return -1
		}
		return persisted[k]
	}

	return []reconciliation{
		{"items", len(src.Items), counts[noteItemSkipped], len(res.Items), inDB("items")},
		{"recipes", len(src.Recipes), counts[noteRecipeSkipped], len(res.Recipes), inDB("recipes")},
		{
			"recipe_ingredients", srcIngs,
			res.SkippedIngredients + counts[noteDuplicateIngredient], builtIngs, inDB("recipe_ingredients"),
		},
		{
			"recipe_categories_recipes", srcCats,
			res.SkippedCategoryLinks + counts[noteCategoryLinkDropped] + counts[noteDuplicateCategory],
			builtCats, inDB("recipe_categories_recipes"),
		},
	}
}

func reconcileOK(res *transformResult, src *source, persisted map[string]int) bool {
	for _, r := range reconcile(res, src, persisted) {
		if r.Source-r.Skipped != r.Built {
			return false
		}
		if r.Persisted >= 0 && r.Persisted != r.Built {
			return false
		}
	}
	return true
}

func noteCounts(res *transformResult) map[string]int {
	c := map[string]int{}
	for _, n := range res.Notes {
		c[n.Kind]++
	}
	return c
}

func exitCode(res *transformResult) int {
	if res.skipped() {
		return 1
	}
	return 0
}

func summary(res *transformResult, src *source, persisted map[string]int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "users: %d migrated, %d left behind\n", len(res.Users), len(src.Users)-len(res.Users))
	fmt.Fprintf(&b, "item_allowed_units: %d rows\n\n", countAllowedUnits(res))

	b.WriteString("reconciliation:\n")
	b.WriteString("  entity                       source  skipped   built   in-db\n")
	for _, r := range reconcile(res, src, persisted) {
		db := "n/a"
		if r.Persisted >= 0 {
			db = strconv.Itoa(r.Persisted)
		}
		flag := ""
		if r.Source-r.Skipped != r.Built {
			flag = "  <-- MISMATCH: source - skipped != built"
		} else if r.Persisted >= 0 && r.Persisted != r.Built {
			flag = "  <-- MISMATCH: built != in-db"
		}
		fmt.Fprintf(&b, "  %-26s %6d %8d %7d %7s%s\n", r.Entity, r.Source, r.Skipped, r.Built, db, flag)
	}

	counts := noteCounts(res)
	if len(counts) > 0 {
		b.WriteString("\nadjustments and skips:\n")
		for _, kind := range sortedKeysInt(counts) {
			fmt.Fprintf(&b, "  %-26s %d\n", kind, counts[kind])
		}
		b.WriteString("\ndetail (unit-blank omitted - the old unitless convention, expected):\n")
		for _, n := range res.Notes {
			if n.Kind == noteUnitBlank {
				continue
			}
			fmt.Fprintf(&b, "  [%s] %s: %s\n", n.Kind, n.Entity, n.Detail)
		}
	}

	if res.skipped() {
		b.WriteString("\nrows were skipped - review the detail, fix the source or accept the gap, then rerun.\n")
	} else {
		b.WriteString("\nnothing skipped.\n")
	}
	return b.String()
}

func countAllowedUnits(res *transformResult) int {
	n := 0
	for _, it := range res.Items {
		n += len(it.AllowedUnitIDs)
	}
	return n
}

func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
