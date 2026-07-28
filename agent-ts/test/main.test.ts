import { describe, expect, test } from "bun:test";

import { finalizationDeadlines } from "../src/main.js";

describe("finalizationDeadlines", () => {
  test("reserves the final five seconds of a managed job for terminal completion", () => {
    const now = 1000;
    expect(finalizationDeadlines(now, 20_000)).toEqual({ workDeadline: 16_000, completionDeadline: 21_000 });
  });
});
