package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	os.Exit(run())
}

func run() int {
	sourceDir := flag.String("source", "", "directory holding the crockpotV3.*.json Compass exports")
	yes := flag.Bool("yes", false, "actually run; without it, print the target and what would change, then stop")
	allowProd := flag.Bool("allow-prod", false, "permit running when MIGRATE_ALLOW is not \"dev\"")
	ignoreAllowed := flag.Bool("ignore-item-allowed-units", false, "import every item unconstrained (no item_allowed_units rows)")
	allowMissingSub := flag.Bool("allow-missing-google-sub", false, "insert Francis/Zoe with a placeholder google_id if Account.json lacks their sub")
	flag.Parse()

	if *sourceDir == "" {
		return fail(errors.New("--source is required"))
	}

	allow := os.Getenv("MIGRATE_ALLOW")
	if allow == "" {
		return fail(errors.New("refusing to run: set MIGRATE_ALLOW in the environment first"))
	}
	if allow != "dev" && !*allowProd {
		return fail(fmt.Errorf("MIGRATE_ALLOW=%q is not \"dev\"; pass --allow-prod to run against it", allow))
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fail(errors.New("DATABASE_URL is not set"))
	}

	src, err := loadSource(*sourceDir)
	if err != nil {
		return fail(err)
	}

	ctx := context.Background()
	pool, err := openPool(ctx, databaseURL)
	if err != nil {
		return fail(err)
	}
	defer pool.Close()

	seeded, err := fetchSeeded(ctx, pool)
	if err != nil {
		return fail(err)
	}

	ic, un, rc, misses := buildResolvers(src, seeded)
	if len(misses) > 0 {
		return fail(referenceMismatchError(misses))
	}

	res, err := transform(transformInput{
		src:             src,
		itemCategories:  ic,
		units:           un,
		recipeCategs:    rc,
		unitIDByName:    seeded.units,
		accountSubs:     accountSubs(src.Accounts),
		ignoreAllowed:   *ignoreAllowed,
		allowMissingSub: *allowMissingSub,
	})
	if err != nil {
		return fail(err)
	}

	host, dbName := describeTarget(databaseURL)
	if !*yes {
		fmt.Printf("DRY RUN (MIGRATE_ALLOW=%s)\n", allow)
		fmt.Printf("  target host: %s\n  target db:   %s\n", host, dbName)
		fmt.Println("  would truncate items, recipes, and every table that FKs into them")
		fmt.Println("  (favourites, planner entries, menu history, shopping-list items), delete 3 users, then insert:")
		fmt.Println("  re-run with --yes to actually write.")
		fmt.Println()
		fmt.Print(summary(res, src, nil))
		return exitCode(res)
	}

	fmt.Printf("running against %s / %s\n\n", host, dbName)
	persisted, err := load(ctx, pool, res)
	if err != nil {
		return fail(err)
	}

	fmt.Print(summary(res, src, persisted))
	code := exitCode(res)
	if !reconcileOK(res, src, persisted) {
		fmt.Fprintln(os.Stderr, "migrate-data: reconciliation mismatch - see the table above")
		code = 1
	}
	return code
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "migrate-data:", err)
	return 1
}

func referenceMismatchError(misses []refMiss) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%d reference name(s) from Mongo have no seeded match:\n", len(misses))
	for _, m := range misses {
		fmt.Fprintf(&b, "  [%s] %q\n      seeded: %s\n", m.Kind, m.Name, strings.Join(m.SeededNames, ", "))
	}
	b.WriteString("reconcile the seed migration or the source data, then rerun (nothing was written)")
	return errors.New(b.String())
}

func describeTarget(databaseURL string) (host, dbName string) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "?", "?"
	}
	return u.Host, strings.TrimPrefix(u.Path, "/")
}
