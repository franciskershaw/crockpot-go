package repository_test

import (
	"context"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipeVisibleTo_TruthTable(t *testing.T) {
	creator := uuid.New()
	other := uuid.New()

	cases := []struct {
		name          string
		approved      bool
		createdByID   *uuid.UUID
		callerID      *uuid.UUID
		callerIsAdmin bool
		want          bool
	}{
		{"approved, other caller", true, &creator, &other, false, true},
		{"approved, anonymous", true, &creator, nil, false, true},
		{"approved, deleted creator, anonymous", true, nil, nil, false, true},
		{"unapproved, admin", false, &creator, &other, true, true},
		{"unapproved, creator", false, &creator, &creator, false, true},
		{"unapproved, other caller", false, &creator, &other, false, false},
		{"unapproved, anonymous", false, &creator, nil, false, false},
		{"unapproved, deleted creator, other caller", false, nil, &other, false, false},
		{"unapproved, deleted creator, anonymous", false, nil, nil, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got *bool
			err := db.DB.QueryRow(context.Background(),
				`SELECT recipe_visible_to($1::boolean, $2::uuid, $3::uuid, $4::boolean)`,
				tc.approved, tc.createdByID, tc.callerID, tc.callerIsAdmin,
			).Scan(&got)
			require.NoError(t, err)
			require.NotNil(t, got, "recipe_visible_to returned NULL")
			assert.Equal(t, tc.want, *got)
		})
	}
}
