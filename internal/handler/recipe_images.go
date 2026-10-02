package handler

import "strings"

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
