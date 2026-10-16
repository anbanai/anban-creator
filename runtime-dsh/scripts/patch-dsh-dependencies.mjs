import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const runtimeDir = resolve(fileURLToPath(new URL("..", import.meta.url)));
const targetPath = resolve(runtimeDir, "node_modules/@deepseek-ai/dsh-llm/lib/index.js");
const marker = "Anban transport-cause preservation patch";
const before = `\tif (carried !== void 0 && carried.code === ownErrorCode(error)) return carried;\n\treturn Object.freeze({\n\t\tmessage: errorMessage(error),\n\t\tcode: harnessErrorCode(error)\n\t});`;
const after = `\t// ${marker}: the provider adapter keeps the underlying network cause\n\t// on LlmError, but the generic failure snapshot otherwise drops it.\n\tconst transportFailure = ownErrorCode(error) === "TRANSPORT";\n\tif (carried !== void 0 && carried.code === ownErrorCode(error)) {\n\t\treturn transportFailure ? Object.freeze({ ...carried, message: errorChain(error) }) : carried;\n\t}\n\treturn Object.freeze({\n\t\tmessage: transportFailure ? errorChain(error) : errorMessage(error),\n\t\tcode: harnessErrorCode(error)\n\t});`;

const checkOnly = process.argv.includes("--check");
const source = readFileSync(targetPath, "utf8");
if (source.includes(marker)) process.exit(0);
if (!source.includes(before)) {
  throw new Error(`unsupported @deepseek-ai/dsh-llm alpha.4 layout: ${targetPath}`);
}
if (checkOnly) {
  throw new Error(`@deepseek-ai/dsh-llm transport-cause patch is not applied: ${targetPath}`);
}
writeFileSync(targetPath, source.replace(before, after), "utf8");
