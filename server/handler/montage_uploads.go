package handler

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func isMontageProjectForUser(ctx context.Context, repo repository.Repository, userID, projectID string) bool {
	if repo == nil || userID == "" || projectID == "" {
		return false
	}
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil || project.UserID != userID {
		return false
	}
	return model.IsMontagePlatform(project.Platform)
}

func hypitAssetURLs(in *model.HypitInput) []string {
	if in == nil {
		return nil
	}
	a := append([]model.HypitAsset(nil), in.SourceAssets...)
	if in.Reference != nil {
		a = append(a, *in.Reference)
	}
	var urls []string
	for _, v := range a {
		if v.URL != "" {
			urls = append(urls, v.URL)
		}
	}
	return urls
}
func rewriteHypitAssetURLs(in *model.HypitInput, m map[string]string) {
	if in == nil {
		return
	}
	if in.Reference != nil {
		in.Reference.URL = rewriteFinalizedUploadURL(in.Reference.URL, m)
	}
	for i := range in.SourceAssets {
		in.SourceAssets[i].URL = rewriteFinalizedUploadURL(in.SourceAssets[i].URL, m)
	}
}
