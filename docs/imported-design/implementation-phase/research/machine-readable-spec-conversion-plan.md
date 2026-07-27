# Machine-Readable Spec Conversion Plan

## Purpose

This plan converts the frozen prose specs into implementation-facing artifacts that code and tests can consume directly.

## Conversion order

1. `effectspec.schema.json`
2. `transaction-states.json`
3. `effect-states.json`
4. `transition-rules.json`
5. `approval-snapshot.schema.json`
6. `metrics.schema.json`
7. canonical resource normalization test vectors

## Rules

- prose remains authoritative for meaning;
- machine-readable files become authoritative for coordinator/test inputs;
- every JSON artifact needs companion validation tests;
- no adapter or coordinator code should invent fields outside these contracts.

## First-pass contents

### 1. EffectSpec schema
Must encode:
- support levels;
- lifecycle support flags;
- metadata fields;
- error classes;
- resource URI pattern declarations.

### 2. Transaction/effect state files
Must encode:
- allowed states;
- terminal states;
- allowed transitions;
- transition guard identifiers.

### 3. Approval snapshot schema
Must encode:
- top-level hashes;
- effect binding entries;
- approver records;
- invalidation-relevant fields.

### 4. Metrics schema
Must encode:
- benchmark metric names;
- units where relevant;
- required versus optional metrics.

### 5. Resource normalization vectors
Must provide:
- valid canonical examples;
- equivalent input variants;
- expected normalized outputs;
- invalid examples.

## Output location

```text
specs/
├── effectspec/
├── transactions/
├── approval/
├── resources/
└── benchmarks/
```

## Definition of done

This plan is complete when the repo has:
- the files above;
- schema validation tests;
- transition table validation tests;
- resource normalization fixtures.
