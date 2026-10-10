import { describe, expect, test } from "bun:test";

import { parseJobConfig } from "../src/config.js";

describe("parseJobConfig", () => {
  test("accepts the Docker dispatcher job contract", () => {
    expect(
      parseJobConfig([
        "job",
        "--server-url",
        "https://creator.example.com",
        "--execution-id",
        "execution-1",
        "--workspace",
        "/workspace",
        "--workload-token-file",
        "/run/secrets/anban/token",
      ]),
    ).toEqual({
      serverURL: "https://creator.example.com",
      executionID: "execution-1",
      workspace: "/workspace",
      workloadTokenFile: "/run/secrets/anban/token",
      allowHTTPServer: false,
    });
  });

  test("rejects an insecure server URL unless explicitly allowed", () => {
    expect(() =>
      parseJobConfig([
        "job",
        "--server-url",
        "http://creator.example.com",
        "--execution-id",
        "execution-1",
        "--workload-token-file",
        "/run/secrets/anban/token",
      ]),
    ).toThrow("server URL must use HTTPS");
  });
});
