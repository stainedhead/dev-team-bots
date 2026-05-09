# Claude Code Usage Report — dev-team-bots

**Period:** 2026-04-07 → 2026-05-07 (30 days)
**Generated:** 2026-05-07

---

## Summary

| Metric | Value |
|---|---|
| **Total estimated cost** | **$381.38** |
| Sessions scanned | 289 |
| Web search requests | 0 |
| Active days | 5 (May 3–7) |

---

## Cost by Model

| Model | Input | Output | Cache Write | Cache Read | Web Search | **Total** |
|---|---|---|---|---|---|---|
| claude-sonnet-4-6 | $0.26 | $74.96 | $76.46 | $227.11 | $0.00 | **$378.78** |
| claude-haiku-4-5-20251001 | $0.00 | $0.20 | $1.21 | $1.18 | $0.00 | **$2.59** |
| **Total** | **$0.26** | **$75.16** | **$77.67** | **$228.29** | **$0.00** | **$381.38** |

> **Note:** Model `<synthetic>` appeared in 289 sessions but carried zero tokens — likely a placeholder or internal model ID. Fallback pricing was applied but no cost was incurred.

### Model Mix

- **claude-sonnet-4-6:** 99.3% of spend ($378.78)
- **claude-haiku-4-5-20251001:** 0.7% of spend ($2.59)

---

## Cost by Repository

| Repository | Cost | Share |
|---|---|---|
| `dev-team-bots` (root) | $334.01 | 87.5% |
| `boabot` | $36.10 | 9.5% |
| `boabotctl` | $5.72 | 1.5% |
| `boabot-team/bots` | $5.55 | 1.5% |

---

## Daily Breakdown

| Date | Input | Output | Cache Write | Cache Read | Web Search | **Total** |
|---|---|---|---|---|---|---|
| 2026-05-03 | $0.00 | $0.52 | $0.26 | $0.68 | $0.00 | $1.46 |
| 2026-05-04 | $0.00 | $1.99 | $1.23 | $4.90 | $0.00 | $8.13 |
| 2026-05-05 | $0.15 | $15.17 | $15.22 | $34.67 | $0.00 | $65.21 |
| 2026-05-06 | $0.08 | $23.99 | $26.98 | $68.54 | $0.00 | $119.60 |
| 2026-05-07 | $0.02 | $33.48 | $33.98 | $119.50 | $0.00 | $186.98 |
| **Total** | **$0.26** | **$75.16** | **$77.67** | **$228.29** | **$0.00** | **$381.38** |

---

## Notable Patterns

1. **Concentrated activity:** All usage occurred in a single 5-day window (May 3–7). The prior 25 days of the month had zero sessions. This suggests a focused development sprint or feature implementation period.

2. **Exponential daily growth:** Daily cost grew from $1.46 on May 3 to $186.98 on May 7 — a 128× increase over 5 days. The last two days alone account for 78% of the monthly total.

3. **Cache reads dominate spend:** $228.29 (59.9% of total) came from cache reads, followed by cache writes at $77.67 (20.4%) and output tokens at $75.16 (19.7%). Input tokens are negligible at $0.26 (0.07%). This pattern is consistent with long-running sessions where context is repeatedly read back.

4. **Sonnet-4-6 is the workhorse:** 99.3% of all cost is on claude-sonnet-4-6. Haiku usage is minimal ($2.59), likely for lightweight tasks or quick checks.

5. **Root repo drives 87.5% of spend:** The `dev-team-bots` root directory accounts for the vast majority of cost, consistent with the monorepo structure where most work touches multiple modules.

6. **No web search usage:** Zero web search requests were made during this period.

---

## Token Volume

| Token Type | Total |
|---|---|
| Input tokens | 86,885 |
| Output tokens | 5,047,870 |
| Cache creation (write) tokens | 21,598,300 |
| Cache read tokens | 771,795,306 |
| Web search requests | 0 |

---

*Report generated from local Claude Code session data (`~/.claude/projects/`) using `cost_analyzer.py`. Costs are estimates based on Anthropic's token-based API pricing from `resources/prices.json`.*
