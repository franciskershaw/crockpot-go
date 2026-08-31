package main

import (
	"sort"
	"strings"

	"github.com/google/uuid"
)

type refRow struct {
	ID   oid
	Name string
}

type refMiss struct {
	Kind        string
	Name        string
	SeededNames []string
}

// refResolver maps a source reference _id to the seeded Postgres row's UUID.
type refResolver struct {
	byMongoID map[oid]uuid.UUID
	dropped   map[oid]bool
}

func objectIDToUUID(o oid) uuid.UUID {
	return uuid.NewSHA1(migrationNamespace, []byte(o))
}

func buildRefResolver(kind string, rows []refRow, seeded map[string]uuid.UUID) (*refResolver, []refMiss) {
	res := &refResolver{
		byMongoID: make(map[oid]uuid.UUID, len(rows)),
		dropped:   map[oid]bool{},
	}
	var misses []refMiss
	for _, r := range rows {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			res.dropped[r.ID] = true
			continue
		}
		id, ok := seeded[name]
		if !ok {
			misses = append(misses, refMiss{Kind: kind, Name: name, SeededNames: sortedKeys(seeded)})
			continue
		}
		res.byMongoID[r.ID] = id
	}
	return res, misses
}

func accountSubs(accounts []mongoAccount) map[oid]string {
	out := make(map[oid]string)
	for _, a := range accounts {
		if a.Provider == "google" && a.ProviderAccountID != "" {
			out[a.UserID] = a.ProviderAccountID
		}
	}
	return out
}

func sortedKeys(m map[string]uuid.UUID) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
