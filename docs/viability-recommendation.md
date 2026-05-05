# BaoBot Dev Team — Viability Recommendation

**Date:** 2026-05-05  
**Author:** Orchestrator (BaoBot M8 output)  
**Status:** Initial recommendation — to be updated with live operational data after 4 weeks of deployment

---

## Executive Summary

The BaoBot cooperative AI dev-team model is **viable for solo-developer adoption** with meaningful caveats. The architecture delivers on its core promise — continuous development progress during offline hours — and the cost model is acceptable for a single-developer context. Company-wide adoption requires further validation of multi-team coordination, access control at scale, and cost predictability under concurrent workloads.

**Recommendation: Proceed with solo deployment. Reassess company-wide rollout after 30 days of operational data.**

---

## Assessment Dimensions

### 1. Throughput and Delivery Accuracy

**Hypothesis:** AI agents operating autonomously can sustain meaningful development progress across multiple parallel workflow stages (backlog → spec → implement → review → done).

**Architecture support:**
- 11-step workflow with role-gated progression enforces quality checkpoints
- ETA estimator calibrates against observed completion times (seed 0.015 AI-min/human-man-day; calibration at 10 samples)
- Per-step cycle time and throughput metrics captured via `MetricsStore`
- Weekly viability summary delivered via SNS

**Gaps to validate:** Actual throughput numbers, task abandonment rate, and ETA accuracy require live operational data. The 0.015 seed ratio is theoretical and may vary significantly by task type.

### 2. Cost Model

**Budget controls implemented:**
- Per-bot daily token cap enforced via `domain.BudgetTracker` (checked before every model invocation)
- Hourly tool-call cap with hard gate (invocation blocked if cap exceeded)
- DynamoDB-backed spend tracking with 30-second flush interval survives restarts
- Spike alert (threshold-pct) and flat-cap alert (threshold-pct) via SNS
- System-level monthly USD cap surfaced in `DailyCostReviewUseCase`

**Estimated cost range (pre-operational):**
- A 5-bot team at claude-sonnet-4-6 rates, 8h/day autonomous operation: approximately $30–$90/month depending on task complexity and tool usage density
- Cost is strongly driven by context window size per task — the context threshold trim (configurable per-bot) is the primary cost lever

**Gaps to validate:** Actual $/task and $/workflow-completion ratios require live data. The 30-day operational period should produce enough samples to calibrate both ETA and cost models.

### 3. Workflow Quality

**Architecture support:**
- Mandatory review gate before every implementation phase (document_prd → review_prd → spec → implement → code_design_review → remediate)
- Orchestrator-controlled workflow routing with hot-reload via SIGHUP
- Stalled-item recovery (configurable staleness threshold; reassigns to next eligible bot)
- Content screening on all inbound instructions (injection pattern detection)

**Key risk:** The model-quality ceiling is determined by the provider (Claude Sonnet 4.6). Complex tasks requiring sustained multi-file reasoning may require human checkpoints not currently gated by the workflow. The `confirmation` step exists for this purpose but is not auto-triggered — it requires operator judgement to invoke.

### 4. Operational Complexity

**Solo-developer operational cost:**
- `baobotctl` CLI covers all daily operations: board management, team status, DLQ recovery, user admin
- Kanban board HTMX UI provides at-a-glance team status without CLI
- SIGHUP hot-reload allows workflow changes without CDK redeploy
- Alert routing via SNS → email/Slack means no polling required

**Anticipated daily overhead:** 5–15 minutes of review and direction, primarily reviewing completed work items and moving stalled items.

### 5. Infrastructure Risk

**AWS ECS Fargate deployment:**
- Stateless containers restart cleanly; budget counters are seeded from DynamoDB on startup
- Aurora Serverless v2 scales to zero during idle periods (cost efficiency)
- Per-bot SQS queues + DLQ provide isolation: one stalled bot cannot block others
- S3 team memory bucket is append-only; no destructive operations from bot code paths

**Single-tenant risk:** All bots share the same RDS Aurora cluster. A schema migration affecting one bot type affects all bots simultaneously. Mitigation: schema migrations require orchestrator quiesce + ECS rolling deploy.

---

## Decision Table

| Criterion | Status | Signal |
|---|---|---|
| Throughput meets solo dev needs | **TBD** | Requires 30-day live data |
| Cost per task is predictable | **Moderate** | Budget controls exist; actual $/task unknown |
| Delivery accuracy (ETA) | **TBD** | Seed ratio set; needs calibration data |
| Operational overhead acceptable | **Low** | CLI + web UI design supports <15 min/day |
| Infrastructure stability | **High** | ECS + Aurora + DLQ recovery proven patterns |
| Security (auth + screening) | **High** | JWT + bcrypt + injection screening implemented |
| Company-wide readiness | **Not ready** | Multi-tenancy and RBAC at scale not validated |

---

## Recommended Measurement Protocol

For the first 30 days of deployment, capture:

1. **Weekly**: workflow completion rate (items reaching `done` / items created), average cycle time per step
2. **Weekly**: total cost (USD) and cost per completed item
3. **Weekly**: ETA accuracy (predicted vs. actual completion time, % within ±25%)
4. **As-occurred**: DLQ count and recovery rate
5. **As-occurred**: stalled-item count and auto-recovery success rate

These metrics are exposed via `baobotctl board list`, `baobotctl team health`, and the weekly SNS viability summary.

After 30 days, revisit:
- Whether the seed ETA multiplier should be adjusted per task type
- Whether any workflow step has a disproportionate cycle time (candidate for additional bot assignment)
- Whether the cost model requires per-task caps in addition to daily/monthly caps

---

## Company-Wide Adoption Prerequisites

Before recommending company-wide adoption, the following must be addressed:

1. **Multi-tenancy:** Current architecture is single-tenant (one team per deployment). Company-wide requires per-team isolation (separate ECS clusters or namespace-scoped IAM).
2. **RBAC at scale:** Current auth supports `admin`/`user` roles. Company teams require project-scoped permissions.
3. **Skill library governance:** Skills are approved per-bot-type; a shared skill library needs versioning and cross-team approval workflow.
4. **Compliance audit:** SQS message bodies may contain code snippets or customer-adjacent content. Data classification and retention policies need review before company-wide deployment.
5. **Operational runbook:** A documented runbook for DLQ recovery, budget overrun response, and bot incident response is required before handing off to non-technical operators.

---

## Conclusion

The BaoBot architecture is technically sound and appropriately scoped for solo-developer use. The budget controls, workflow quality gates, and operational tooling are production-ready. The viability hypothesis — that an AI agent team can sustainably accelerate solo development — is plausible but unproven; the 30-day measurement protocol will produce the evidence needed to either confirm or refine it.

**Next review date:** 30 days after production deployment.
