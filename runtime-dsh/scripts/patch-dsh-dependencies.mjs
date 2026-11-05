import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const runtimeDir = resolve(fileURLToPath(new URL("..", import.meta.url)));
const transportTargetPath = resolve(runtimeDir, "node_modules/@deepseek-ai/dsh-llm/lib/index.js");
const malformedToolInputTargetPath = resolve(runtimeDir, "node_modules/@deepseek-ai/dsh-llm-deepseek/lib/index.js");
const marker = "Anban transport-cause preservation patch";
const malformedToolInputMarker = "Anban malformed-tool-input recovery patch";
const before = `\tif (carried !== void 0 && carried.code === ownErrorCode(error)) return carried;\n\treturn Object.freeze({\n\t\tmessage: errorMessage(error),\n\t\tcode: harnessErrorCode(error)\n\t});`;
const after = `\t// ${marker}: the provider adapter keeps the underlying network cause\n\t// on LlmError, but the generic failure snapshot otherwise drops it.\n\tconst transportFailure = ownErrorCode(error) === "TRANSPORT";\n\tif (carried !== void 0 && carried.code === ownErrorCode(error)) {\n\t\treturn transportFailure ? Object.freeze({ ...carried, message: errorChain(error) }) : carried;\n\t}\n\treturn Object.freeze({\n\t\tmessage: transportFailure ? errorChain(error) : errorMessage(error),\n\t\tcode: harnessErrorCode(error)\n\t});`;

const malformedToolInputBefore = `\t\t\tif (reason.kind !== "max-tokens") for (const { content } of blocks.values()) {\n\t\t\t\tif (content.type !== "tool-call") continue;\n\t\t\t\tlet parsed;\n\t\t\t\ttry {\n\t\t\t\t\tparsed = JSON.parse(content.arguments);\n\t\t\t\t} catch (_invalidProviderToolJson) {\n\t\t\t\t\treturn malformed("tool input is invalid JSON");\n\t\t\t\t}\n\t\t\t\tobject(parsed);\n\t\t\t}`;
const malformedToolInputAfter = `\t\t\t// ${malformedToolInputMarker}: preserve truncated arguments so ToolRuntime\n\t\t\t// can return INVALID_ARGS to the model without running a tool body.\n\t\t\tif (reason.kind !== "max-tokens") for (const { content } of blocks.values()) {\n\t\t\t\tif (content.type !== "tool-call") continue;\n\t\t\t}`;

const checkOnly = process.argv.includes("--check");
const transportSource = readFileSync(transportTargetPath, "utf8");
let patchedTransport = transportSource;
if (!patchedTransport.includes(marker)) {
  if (!patchedTransport.includes(before)) {
    throw new Error(`unsupported @deepseek-ai/dsh-llm alpha.2 layout: ${transportTargetPath}`);
  }
  if (checkOnly) {
    throw new Error(`@deepseek-ai/dsh-llm transport-cause patch is not applied: ${transportTargetPath}`);
  }
  patchedTransport = patchedTransport.replace(before, after);
}
if (patchedTransport !== transportSource) writeFileSync(transportTargetPath, patchedTransport, "utf8");
const malformedToolInputSource = readFileSync(malformedToolInputTargetPath, "utf8");
let patchedMalformedToolInput = malformedToolInputSource;
if (!patchedMalformedToolInput.includes(malformedToolInputMarker)) {
  if (!patchedMalformedToolInput.includes(malformedToolInputBefore)) {
    throw new Error(`unsupported @deepseek-ai/dsh-llm-deepseek alpha.2 layout: ${malformedToolInputTargetPath}`);
  }
  if (checkOnly) {
    throw new Error(`@deepseek-ai/dsh-llm-deepseek malformed-tool-input patch is not applied: ${malformedToolInputTargetPath}`);
  }
  patchedMalformedToolInput = patchedMalformedToolInput.replace(malformedToolInputBefore, malformedToolInputAfter);
}
if (patchedMalformedToolInput !== malformedToolInputSource) writeFileSync(malformedToolInputTargetPath, patchedMalformedToolInput, "utf8");
