export function appendResumeContextToPrompt(prompt: string, relativePath?: string): string {
  if (!relativePath) return prompt;
  return `${prompt}\n\n继续执行模式：\n- 这是一个基于原任务工作目录的继续执行，不是全新任务。\n- 请先读取 \`${relativePath}\`，理解用户补充指令、补充文件说明和附件相对路径。\n- 基于当前工作目录已有草稿、素材和产物继续完成任务；不要清空、删除或整体覆盖已有产物，除非补充指令明确要求替换。\n- 如果补充文件中存在同名或相近用途文件，优先按 latest.md 中的文件说明区分使用。`;
}
