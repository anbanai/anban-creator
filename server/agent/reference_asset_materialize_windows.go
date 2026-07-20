//go:build windows

package agent

import (
	"context"
)

func materializeReferenceAssetBytes(ctx context.Context, workDir string, data []byte) error {
	return materializeReferenceAssetWithRoot(ctx, workDir, data)
}
