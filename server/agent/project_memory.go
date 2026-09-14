package agent

import (
	"context"
	"errors"
)

var errProjectMemoryStoreRequired = errors.New("project memory store is required")

// ProjectMemoryStore manages the Server-mounted shared project-memory tree.
type ProjectMemoryStore interface {
	EnsureProject(context.Context, string) error
	RequireProject(context.Context, string) error
}

func prepareProjectMemory(ctx context.Context, store ProjectMemoryStore, projectID string, resume bool) error {
	if store == nil {
		return NewPermanentDispatchError(errProjectMemoryStoreRequired)
	}
	var err error
	if resume {
		err = store.RequireProject(ctx, projectID)
	} else {
		err = store.EnsureProject(ctx, projectID)
	}
	if err != nil {
		return NewPermanentDispatchError(err)
	}
	return nil
}
