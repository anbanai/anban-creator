package handler

import (
	"fmt"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"testing"
)

func TestHypitDoesNotSignUnownedPersistedURL(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	store := &resolveDownloadStore{}
	key := "tasks/another-user/private.mp4"
	task := &model.Task{ID: "review-task", UserID: "review-user", ProjectID: "review-project", Type: model.PlatformHypit}
	task.SetHypitInput(model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video", URL: "/api/v1/files/" + key}})
	if err := repo.Tasks().Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	app, _ := newResolveUploadHandler(t, repo, store, task.UserID)
	resp, body := resolveDownloadRequest(t, app, fmt.Sprintf(`{"owner_type":"task","owner_id":%q,"key":%q}`, task.ID, key))
	t.Logf("signed another user's key: HTTP %d body=%v", resp.StatusCode, body)
	if resp.StatusCode == 200 || len(store.calls) != 0 {
		t.Fatal("unowned stored URL granted access")
	}
}
