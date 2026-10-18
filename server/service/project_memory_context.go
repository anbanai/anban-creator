package service

import (
	"context"
	"fmt"

	projectmemory "github.com/anbanai/anban-creator/server/memory"
)

var projectMemoryContextPaths = []string{
	"AGENTS.md",
	"profile/identity.md",
	"profile/style.md",
	"profile/audience.md",
	"profile/platforms.md",
	"profile/preferences.md",
	"profile/memory.md",
	"MEMORY.md",
}

func readProjectMemoryContext(ctx context.Context, reader ProjectMemoryReader, projectID string) (map[string]projectmemory.FileView, error) {
	if reader == nil {
		return nil, fmt.Errorf("project memory reader is unavailable")
	}
	view, err := reader.ReadProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read project memory: %w", err)
	}
	files := make(map[string]projectmemory.FileView, len(view.Files))
	for _, file := range view.Files {
		if file.Truncated {
			continue
		}
		files[file.Path] = file
	}
	return files, nil
}

func readProjectBootstrapFiles(ctx context.Context, reader ProjectMemoryReader, projectID string) ([]BootstrapFile, error) {
	contents, err := readProjectMemoryContext(ctx, reader, projectID)
	if err != nil {
		return nil, err
	}
	files := make([]BootstrapFile, 0, len(contents))
	for _, path := range projectMemoryContextPaths {
		if file, ok := contents[path]; ok {
			files = append(files, BootstrapFile{Path: path, Text: file.Content, Mode: 0644})
		}
	}
	return files, nil
}
