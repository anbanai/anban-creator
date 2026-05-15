# Anban 智能创作助手 Online Service — Design Spec

**Date**: 2026-04-02
**Status**: Approved

## Problem

Anban 智能创作助手's content generation capabilities (种草笔记, 公众号图文, 小绿书) are only accessible via CLI within Claude Code. This limits usage to developers with local setup. The goal is to make these capabilities available as a multi-user SaaS web service.

## Design Decisions

1. **Architecture**: Lobswarm pattern — Go/Fiber + Asynq + MySQL + Redis + React/Vite frontend
2. **Project structure**: Extend within anbanwriter repo, add `server/`, `web/`, `claudecode/` directories
3. **Agent execution**: claude-agent-sdk-go with in-process MCP tools (not CLI subprocess)
4. **CLI replacement**: MCP replaces CLI for both server-side agents and Claude Code
5. **Login**: WeChat QR code + password login (anban.codex pattern)
6. **Users**: Multi-user SaaS with per-user WeChat credentials
7. **Scheduling**: Asynq cron for timed plans + manual task creation
8. **Progress**: SSE streaming of Claude's thinking/actions to frontend
9. **Frontend core**: Timeline view showing past tasks + current progress + future scheduled plans

## Architecture

See implementation plan at `.claude/plans/quirky-baking-flask.md` for full details including:
- Directory structure
- Data models (users, user_configs, plans, tasks, task_files)
- MCP tool definitions
- Agent execution flow
- API endpoints
- Login system
- Frontend pages
- Implementation phases (5 phases)

## Key Design Principles

- **Unified MCP interface**: All anbanwriter capabilities exposed via MCP tools, shared between SDK agents and Claude Code
- **Reuse app/ packages**: image, draft, converter, writer, humanizer, wechat packages imported directly
- **Per-user config injection**: MCP tools construct user-specific Config instances from DB records
- **Real-time progress**: SSE pushes Claude's thinking/tool use to the frontend timeline
- **Agent prompt adaptation**: Existing agents/*.md and skills/ reference docs compiled into SDK system prompts
