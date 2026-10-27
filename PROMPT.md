# License Expiration Monitoring

## Context

Every clinician in the ComPsych provider network holds at least one state license, and every
license expires. A provider whose license lapses must not keep seeing members — that's a
regulatory problem, not a cleanup task.

Today expiration is a date sitting in a row. Nothing watches it. You're building the thing
that watches it.

## What to build

A system that tracks license expiration dates and surfaces the licenses approaching expiry.

Intake captures:

| Field | Type | Notes |
|---|---|---|
| `npi` | string | 10 digits |
| `first_name` | string | |
| `last_name` | string | |
| `license_number` | string | |
| `license_state` | string | 2-letter |
| `license_expiration` | date | |

**A provider may hold more than one license** — different states, different expiration dates.
Each is tracked on its own.

Interface is your call — API, CLI. We care about what's behind it.

## Data

We will provide you with a set of data to use during the interview

## Persistence

You don't need a real database, and we'd rather you didn't spend the session wiring one up.

**Before you build, sketch the schema** you would create if this were running on Postgres:
the tables, their keys, and their columns. Talk us through it. Then build that design on
in-memory storage, and treat that storage as durable across runs.

## Windows

Each license sits in exactly one window:

| Window | Meaning |
|---|---|
| `OK` | More than 90 days out |
| `EXPIRING_90` | Within 90 days |
| `EXPIRING_60` | Within 60 days |
| `EXPIRING_30` | Within 30 days |
| `EXPIRED` | Past the expiration date |

The 90/60/30 thresholds are our current policy, not a law of nature. Compliance will want to
change them.

## Output

Whatever you build, we'd like to be able to read a license and its window as JSON in this shape:

```json
{"npi":"1234567890","license_number":"NY-44821","license_state":"NY",
 "license_expiration":"2027-05-30","window":"EXPIRING_90"}
```

That's the only thing we're prescribing about the interface. How you expose it is up to you.

## Requirements

These are what we'll actually check. Everything else is your judgment.

1. **Query by window.** For a given date, list the licenses in each window.
2. **Thresholds are configuration,** not literals in the code.
3. **Nothing is missed.** If the system doesn't run for several days, no license silently skips
   past a window it should have surfaced.
4. **No repeats.** A license shouldn't be surfaced over and over for the same window it's
   already been surfaced for.
5. **Demonstrable movement.** At the end of the session we'll ask you to show one license moving
   `OK → EXPIRING_90 → EXPIRING_60 → EXPIRING_30 → EXPIRED`, live, without waiting 90 days and
   without you editing the system clock by hand. Build toward being able to do that.

Requirement 5 is not a trick. Plan for it early — candidates who leave it to the last ten
minutes generally can't do it.

## Scale

Roughly 40,000 providers and 55,000 licenses today, growing a few hundred a month. Not a large
dataset. Let that inform what you reach for.

## What you don't need to build

Notifications, emails, re-verification against a licensing board, or a human review queue.
Surfacing the licenses is enough.

If you think one of those changes the design underneath, say so — but don't build it.

## How we work

**Use AI agents as your primary development tool.** We expect the agent to write most of the
code. We're evaluating how you direct it, review it, and decide whether to trust it — not your
typing speed.

Work out loud. We want to hear:

- Your schema, and why it's shaped that way.
- How you're modeling license state, and when expiration status gets evaluated.
- How you keep providers from being missed as time passes.
- What you hand to the agent, and the context and constraints you give it.
- How you check what comes back — and what made you look closer at a particular piece.
- Risks and tradeoffs as you hit them.

Narrating while you work is a skill; it's fine to pause and think.

## Scope

**You will not finish all of this. That's by design.** Choose what matters and say why. A narrow
slice you can defend beats broad coverage you can't.

Treat it as code that could reach production but we are okay with making assumptions. Areas worth your attention:

- Architecture and scheduling
- Date and time handling
- Idempotency and repeated processing
- Testing time-dependent behavior
- Missed runs, retries, partial failure
- Stale data — the expiration date we hold may no longer be the truth
- Observability, and knowing the process is actually running

You are not expected to address all of these in the build. We'll discuss several of them.

## Questions

Ask them. Ambiguity in this brief is not always accidental, and noticing it is worth more than
guessing well. Several things in here are deliberately underspecified, and we would rather
you notice and decide than quietly assume.
