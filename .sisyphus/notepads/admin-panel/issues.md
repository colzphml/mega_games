# Issues (admin-panel)

## 2026-02-01 Task: orchestration
- Several subagent runs unexpectedly modified `MEGA_games.csv` and `discord_tools/seed.sql` (out of scope). Restored both files back to HEAD.
- Some delegate_task executions returned "JSON Parse error: Unexpected EOF"; keep delegation prompts shorter and avoid massive boilerplate.
