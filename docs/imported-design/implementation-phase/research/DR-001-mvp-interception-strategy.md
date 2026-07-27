# DR-001 — MVP Interception Strategy

## Status

Accepted for MVP research phase.

## Decision ID

`DR-001`

## Topic

Primary interception path for FutureDiff MVP.

## Decision

FutureDiff MVP should start with a **wrapper/proxy execution path** as the primary interception strategy.

Concretely, FutureDiff should launch or proxy one agent runtime through a controlled entrypoint that:

- owns the effectful credentials;
- injects only proxied tool access;
- captures tool-call lifecycle events;
- routes side effects through the FutureDiff coordinator; and
- runs local work inside the staged runtime boundary.

## Alternatives considered

### 1. Wrapper / proxy path
Chosen.

### 2. MCP proxy first
Deferred.

### 3. Framework-specific hook/plugin first
Deferred.

## Why this wins for MVP

### Fastest path to a credible demo
A wrapper/proxy path lets FutureDiff control the execution boundary without first depending on every framework’s plugin API quality, hook model, or extension lifecycle.

### Better trust-boundary enforcement
FutureDiff’s core claim depends on owning the effect boundary. A wrapper/proxy path makes it easier to:

- hold credentials centrally;
- restrict direct network egress;
- force tool usage through registered adapters;
- stage filesystem/runtime effects before commit.

### Easier recovery debugging
When commit, retry, or crash recovery misbehaves, a single controlled runtime boundary is much easier to inspect than a mix of framework-native hook behavior.

### Lower framework lock-in than plugin-first
A plugin-first strategy sounds integrated, but it usually creates adapter logic coupled to each framework’s lifecycle semantics. That is the wrong starting constraint for a framework-neutral gateway.

## Why MCP proxy is deferred

MCP proxying is attractive, but it is not the best first move for MVP because:

- it still depends on how fully the agent routes tool usage through MCP;
- non-MCP local actions may still need separate interception;
- debugging mixed MCP and non-MCP behavior adds complexity too early.

MCP should remain a planned integration path after the wrapper/proxy path proves the coordinator and adapter model.

## Why framework hooks/plugins are deferred

Framework hooks are useful later, but they are a bad first dependency because:

- they create framework-specific coupling immediately;
- hook capability differs across agent ecosystems;
- they make FutureDiff’s core guarantees depend on external lifecycle APIs too early.

The engine should stabilize before framework-specific integrations spread.

## MVP implementation shape implied by this decision

The first runnable path should look like this:

```text
user command
-> futurediff run
-> wrapper/proxy entrypoint
-> staged runtime + coordinator
-> registered adapters only
-> approval / abort / recover flow
```

This does not forbid later MCP or plugin support. It only sets the first implementation seam.

## Known tradeoffs

- wrapper/proxy UX may feel less native than framework-specific integration;
- some frameworks may need extra glue for prompt/tool/session passthrough;
- local developer setup may require tighter environment control;
- later MCP integration will still be needed for broader adoption.

## Rejected MVP anti-patterns

Do not do these in the first implementation cycle:

- support wrapper, MCP proxy, and plugin paths at once;
- claim framework neutrality by shipping several shallow integrations instead of one strong path;
- let the agent keep raw provider credentials outside the wrapper boundary.

## Required follow-up spikes

1. **Wrapper boundary spike**
   - prove an agent can run through FutureDiff without a second LLM run for commit.

2. **Credential routing spike**
   - prove GitHub/Slack/Postgres credentials can remain gateway-controlled while the agent still completes useful work.

3. **Tool event capture spike**
   - prove side-effecting tool calls can be turned into transaction/effect events with durable IDs.

## Main risks introduced

- some agent frameworks may be awkward to wrap cleanly;
- shell/file actions may be easier to capture than provider-native actions at first;
- wrapper UX may need extra polish later to feel native.

These are acceptable MVP risks. They are better than starting with fragmented interception semantics.

## Owner

FutureDiff architecture track.

## Date

2026-07-26

## Resulting next research item

Freeze the control-plane stack around this interception choice so coordinator, ledger, and recovery behavior can be built against one clear runtime model.
