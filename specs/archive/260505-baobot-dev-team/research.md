# Research: BaoBot Dev Team

**Feature:** baobot-dev-team
**Date:** 2026-05-05
**Source PRD:** [baobot-dev-team-PRD.md](baobot-dev-team-PRD.md)

---

## Research Questions

Derived from PRD open questions and dependencies:

1. **S3 Vectors GA availability in us-east-1:** Is AWS S3 Vectors generally available in us-east-1? If not, what is the timeline? Should we design for OpenSearch Serverless as the primary vector store instead?

2. **Bedrock model IDs and regional quotas:** Which Claude model IDs are available in Bedrock in us-east-1, and what are the default throughput limits? Do we need quota increase requests before first deploy?

3. **SQS A2A envelope shape:** What is the exact envelope format for A2A-compatible messages over SQS? What fields are required in the envelope (sender, recipient, message type, payload, correlation ID, timestamp)?

4. **ETA cold-start calibration:** What seed multiplier value should be used initially? The PRD states observed range 60x–100x (implying multiplier 0.01–0.02). How should the operator configure this, and how should the transition from seed to observed ratio be surfaced in the UI?

5. **OTel ADOT collector integration:** What is the current recommended pattern for integrating the AWS Distro for OpenTelemetry (ADOT) collector with ECS Fargate? Sidecar or separate service? What IAM permissions does the ADOT collector require?

6. **Existing boabot codebase coverage:** What is the current state of the domain, application, and infrastructure layers in `boabot/`? Which interfaces and implementations already exist vs. need to be created?

7. **GitHub OAuth2 OIDC flow:** Does GitHub's OAuth2 implementation expose a standard OIDC discovery endpoint (`.well-known/openid-configuration`)? How does the JWT issued by our auth provider need to be structured to be compatible with both `baobotctl` and the web UI?

---

## Industry Standards

[TBD — document relevant standards: A2A protocol spec, OIDC/OAuth2 RFCs, OTel specification version, SQS best practices for fan-out/fan-in]

---

## Existing Implementations

[TBD — survey boabot/, boabotctl/, boabot-team/ current state; document what exists vs. gaps]

---

## API Documentation

[TBD — links to: AWS Bedrock API, SQS API, SNS API, S3 API, DynamoDB API, Secrets Manager API, OTel Go SDK, gorilla/mux or chi routing]

---

## Best Practices

[TBD — document: Clean Architecture in Go, TDD patterns for interface-heavy code, ECS Fargate task sizing, DLQ patterns for SQS, JWT best practices]

---

## Open Questions

- Bedrock Guardrails promotion criteria: no trigger condition defined — record and defer.
- React Native client: confirm macOS-first target; Windows corporate target is post-v1.
- Web UI technology choice: not specified in PRD — recommend Go-served HTML (HTMX or minimal JS) for simplicity given single-operator use, or React/Vue if richer interactivity needed.

---

## References

[TBD — populate as research is conducted]
