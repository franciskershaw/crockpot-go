package handler_test

import (
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/handler"
)

func TestImageScope_CanDestroy(t *testing.T) {
	dev := handler.ImageScope{UploadFolder: "dev/recipes", Production: false}
	prod := handler.ImageScope{UploadFolder: "recipes", Production: true}

	tests := []struct {
		name     string
		scope    handler.ImageScope
		publicID string
		want     bool
	}{
		{"dev: own upload folder", dev, "dev/recipes/0b6f3c2e-uuid", true},
		{"dev: legacy Crockpot folder", dev, "Crockpot/karexo3vm57cq2japsfw", false},
		{"dev: legacy recipes folder", dev, "recipes/abc123", false},
		{"dev: folder name as prefix of a sibling", dev, "dev/recipesX/abc", false},
		{"dev: the folder itself", dev, "dev/recipes", false},
		{"dev: nested deeper under own folder", dev, "dev/recipes/sub/abc", true},
		{"prod: own upload folder", prod, "recipes/0b6f3c2e-uuid", true},
		{"prod: legacy Crockpot folder", prod, "Crockpot/karexo3vm57cq2japsfw", true},
		{"prod: dev folder", prod, "dev/recipes/abc", false},
		{"prod: legacy folder name as prefix of a sibling", prod, "Crockpot-old/abc", false},
		{"prod: lowercase crockpot is not the legacy folder", prod, "crockpot/abc", false},
		{"prod: root-level asset", prod, "sample", false},
		{"prod: empty public id", prod, "", false},
		{"dev: empty public id", dev, "", false},
		{"empty upload folder never matches everything", handler.ImageScope{}, "anything/abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.CanDestroy(tt.publicID); got != tt.want {
				t.Errorf("CanDestroy(%q) = %v, want %v", tt.publicID, got, tt.want)
			}
		})
	}
}
