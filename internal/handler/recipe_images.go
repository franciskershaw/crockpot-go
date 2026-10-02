package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/cloudinary"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
)

// legacyImageFolders hold the migrated recipes' photos; only production may destroy them.
var legacyImageFolders = []string{"Crockpot", "recipes"}

// ImageScope is which Cloudinary folders this environment may destroy assets in: dev and prod share one account and the same migrated rows.
type ImageScope struct {
	UploadFolder string
	Production   bool
}

func (s ImageScope) CanDestroy(publicID string) bool {
	if inImageFolder(publicID, s.UploadFolder) {
		return true
	}
	if !s.Production {
		return false
	}
	for _, folder := range legacyImageFolders {
		if inImageFolder(publicID, folder) {
			return true
		}
	}
	return false
}

func inImageFolder(publicID, folder string) bool {
	if folder == "" {
		return false
	}
	rest, ok := strings.CutPrefix(publicID, folder+"/")
	return ok && rest != ""
}

// uploadPhoto applies the per-user photo limit, then uploads into this environment's folder; on failure it has written the response.
func (h *RecipeHandler) uploadPhoto(c *gin.Context, userID string, photo []byte) (*cloudinary.UploadedImage, bool) {
	limit, err := h.images.PhotoLimiter.Get(c.Request.Context(), userID)
	if err != nil {
		internalError(c, "failed to check photo rate limit", err)
		return nil, false
	}
	if limit.Reached {
		middleware.RespondRateLimited(c, max(limit.Reset-time.Now().Unix(), 1))
		return nil, false
	}

	publicID := h.images.Scope.UploadFolder + "/" + h.images.NewID()
	img, err := h.images.Store.Upload(c.Request.Context(), bytes.NewReader(photo), publicID)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to upload photo: %w", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "image_upload_failed"})
		return nil, false
	}
	return &img, true
}

// destroyImage is best-effort: the recipe change has already committed (or rolled back), so a failure is logged, never returned.
func (h *RecipeHandler) destroyImage(c *gin.Context, publicID string) {
	if err := h.images.Store.Destroy(context.WithoutCancel(c.Request.Context()), publicID); err != nil {
		_ = c.Error(fmt.Errorf("failed to destroy image %s: %w", publicID, err))
	}
}

func (h *RecipeHandler) destroyOrphan(c *gin.Context, publicID *string) {
	if publicID != nil && h.images.Scope.CanDestroy(*publicID) {
		h.destroyImage(c, *publicID)
	}
}
