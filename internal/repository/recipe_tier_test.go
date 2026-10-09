package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageTier(t *testing.T) {
	cases := []struct {
		name                     string
		matched, total, selected int
		want                     string
	}{
		{"exactly 55% is best", 11, 20, 11, "best"},
		{"just under 55% is good", 10, 20, 10, "good"},
		{"exactly 25% is good", 5, 20, 5, "good"},
		{"just under 25% has no tier", 4, 20, 4, ""},
		{"no ingredients selected has no tier", 0, 6, 0, ""},
		{"recipe with no ingredients has no tier", 0, 0, 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coverageTier(tc.matched, tc.total, tc.selected)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, *got)
		})
	}
}
