export interface JobConfig {
  serverURL: string;
  executionID: string;
  workspace: string;
  workloadTokenFile: string;
  allowHTTPServer: boolean;
}

function requireValue(args: string[], index: number, flag: string): string {
  const value = args[index + 1];
  if (!value || value.startsWith("--")) {
    throw new Error(`${flag} is required`);
  }
  return value;
}

export function parseJobConfig(args: string[]): JobConfig {
  if (args[0] !== "job") {
    throw new Error("job subcommand is required");
  }

  let serverURL = "";
  let executionID = "";
  let workspace = "/workspace";
  let workloadTokenFile = "";
  let allowHTTPServer = false;

  for (let index = 1; index < args.length; index += 1) {
    switch (args[index]) {
      case "--server-url":
        serverURL = requireValue(args, index, "--server-url");
        index += 1;
        break;
      case "--execution-id":
        executionID = requireValue(args, index, "--execution-id");
        index += 1;
        break;
      case "--workspace":
        workspace = requireValue(args, index, "--workspace");
        index += 1;
        break;
      case "--workload-token-file":
        workloadTokenFile = requireValue(args, index, "--workload-token-file");
        index += 1;
        break;
      case "--allow-http-server":
        allowHTTPServer = true;
        break;
      default:
        throw new Error(`unknown argument ${args[index]}`);
    }
  }

  if (!serverURL || !executionID || !workloadTokenFile) {
    throw new Error("--server-url, --execution-id, and --workload-token-file are required");
  }

  const parsed = new URL(serverURL);
  if (parsed.username || parsed.password || parsed.search || parsed.hash || (parsed.pathname && parsed.pathname !== "/")) {
    throw new Error("server URL is invalid");
  }
  if (parsed.protocol !== "https:" && !(allowHTTPServer && parsed.protocol === "http:")) {
    throw new Error("server URL must use HTTPS");
  }

  return {
    serverURL: parsed.origin,
    executionID,
    workspace,
    workloadTokenFile,
    allowHTTPServer,
  };
}
