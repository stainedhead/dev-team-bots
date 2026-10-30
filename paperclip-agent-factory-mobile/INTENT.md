# INTENT.md — Paperclip Agent Factory Mobile

This file states why the product exists and how to make trade-offs when the PRD is silent. It changes rarely and only with the owner's approval. When a request or design conflicts with this file, stop and ask.

## Purpose

An AI-agent factory run on [Paperclip](https://docs.paperclip.ing) moves only as fast as its slowest human decision. Agents propose; people decide. When the person who decides is away from their desk, the factory waits.

**Paperclip Agent Factory Mobile puts the board operator's decisions, oversight and emergency controls in their pocket, for every Paperclip instance they run, without weakening the factory's security.**

## Who it is for

The owner or operator of one or more Paperclip instances, at home or in the cloud, who is away from the web app and needs to:

1. **Decide** what agents are waiting on: approvals, decisions, questions, budget stops.
2. **Know** whether the factory is healthy: what is running, failing, blocked or overspending.
3. **Intervene** safely: pause an agent, cancel a run, redirect work with a comment.

It is not for configuring agents, authoring documents or administering the instance. That work belongs in the web app.

## Outcomes we measure

- Time from an agent needing a human to the human resolving it, when the human is away from a computer.
- Share of Attention items resolved on mobile rather than waiting for the web app.
- Zero credential exposures: no secret ever stored outside the device secret store or sent anywhere except its own instance.

## Principles, in priority order

When two of these conflict, the earlier one wins.

1. **Security over convenience.** Credentials live only in the Keychain or Keystore. TLS always. Destructive actions need the device owner. We refuse insecure server modes rather than work around them.
2. **Correctness over speed.** Never show stale data as current, never retry a mutation blindly, never guess what an action will do. Show the server's truth, with its timestamp.
3. **A clear decision over more information.** Every screen answers "what needs me, and what happens if I tap this?" Show a decision option's effects before the tap. Cut anything that does not help a decision or an intervention.
4. **Native over uniform.** Each app follows its platform's conventions. Shared behaviour, platform-shaped presentation.
5. **Build once.** Every rule lives in shared Kotlin with a test. The iOS and Android apps render state and forward intents; they never hold business logic.
6. **Many instances are normal.** Every feature works across multiple Paperclip instances and companies. Nothing assumes there is only one.
7. **Respect the user's network.** Home factories stay private (Tailscale, VPN). Notifications are content-free by default. No third-party analytics.

## Boundaries

- The Paperclip REST API is the only backend. If something cannot be done through it, we propose an upstream change or put it behind a port. We do not scrape the web app or reach into its database.
- We do not fork Paperclip.
- The app never runs agents and never holds more authority than the board key the user paired.

## How we build

Test-driven development (red, green, refactor), Clean Architecture with inward-only dependencies, and the full language toolchain (formatters, linters, static analysis, architecture tests, coverage gates) on every change. Details are in [AGENTS.md](./AGENTS.md). Scope is in the current PRD.
