import { describe, expect, test } from "bun:test";

import { appendResumeContextToPrompt } from "../src/resume.js";

describe("appendResumeContextToPrompt", () => {
  test("keeps a non-resume prompt unchanged", () => {
    expect(appendResumeContextToPrompt("base")).toBe("base");
  });

  test("references the execution-scoped supplemental context", () => {
    const path = ".anban-creator/resume/executions/execution-1/latest.md";
    const prompt = appendResumeContextToPrompt("base", path);
    expect(prompt).toContain(`请先读取 \`${path}\``);
    expect(prompt).toContain("不要清空、删除或整体覆盖已有产物");
  });
});
