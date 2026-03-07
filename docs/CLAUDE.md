# docs/CLAUDE.md

Documentation structure for cc-trace.

## Folder Layout

| Folder | Purpose | Format |
|--------|---------|--------|
| `plans/` | Implementation plans -- design decisions and execution steps for features and fixes | `YYYY-MM-DD-<name>-design.md` and `YYYY-MM-DD-<name>-implementation.md` |
| `spikes/` | Research and exploration -- time-boxed investigations to answer technical questions before committing to implementation | `YYYY-MM-DD-<topic>.md` |

## Spike Format

```markdown
# Spike: <Title>
Date: YYYY-MM-DD
Status: open | concluded

## Question
What are we trying to learn?

## Context
Why does this matter now?

## Findings
What did we learn?

## Implications
What does this mean for cc-trace? (potential features, changes, decisions)
```

A spike is **fact-finding, not decision-making**. Findings feed into issues and plans if action is warranted.

## Plan Format

Plans use a two-document pattern:
- **Design doc**: problem, solution approach, trade-offs, files changed
- **Implementation doc**: step-by-step execution checklist
