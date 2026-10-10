import { describe, expect, test } from "bun:test";

import { validateImageArtifactDescriptor } from "../src/downloads.js";

describe("validateImageArtifactDescriptor", () => {
  test("rejects an image descriptor whose path does not match its MIME", () => {
    expect(() => validateImageArtifactDescriptor({ task_file_id: "file-1", file_path: "output/image.jpg", download_url: "https://creator.example.com/files/1", mime_type: "image/png", file_size: 1, content_hash: "a".repeat(64) })).toThrow("must use a .png path");
  });
});
