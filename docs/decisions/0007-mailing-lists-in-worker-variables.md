# 0007 — Mailing lists live in Text variables on one Cloudflare Worker

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
APS email moves to Cloudflare Email Routing in December 2026. Email Routing can forward one
address to one destination, so group addresses (news@, photos@, treasurer@, …) need a Worker that
forwards to many people. The member lists have to be stored somewhere. The tool must be able to
read them (and later update them), and a non-technical future webmaster must be able to see and
fix them.

## Decision
One Worker handles every group address. Each group's members are a **Text** variable named
`LIST_` + the part of the address before the @, in capitals, with non-alphanumerics turned into
`_` (news@ → `LIST_NEWS`), holding comma-separated addresses. Adding a group means adding a
routing rule and a variable, with no code change. The design was built and tested by the webmaster in a
separate Cloudflare account before the switch-over.

The tool reads the lists with a read-only token (Workers Scripts: Read). Its parsing rules in
`internal/connectors/cloudflare/lists.go` deliberately mirror the Worker's code.

## Alternatives considered
- **Addresses written into the Worker's code.** Every change would be a code edit and
  redeploy. Hard for a non-technical successor, and risky to automate.
- **Workers KV storage (one key per member).** Better for frequent automated edits, but the
  lists are less visible in the dashboard and need extra permissions. Can be revisited if the
  lists outgrow variables.
- **A D1 database.** More power than needed.

## Consequences
- Lists are visible and editable in the dashboard (Worker → Settings → Variables and Secrets).
- Variables must be **Text**, not Secret, or the tool can't read them.
- Cloudflare limits variable size (believed to be about 5 KB each, roughly 150–200 addresses). Fine for
  APS today, and worth re-checking as membership grows.
- Everyone on a list must also be a verified destination address. `cloudflare discover` counts
  anyone who isn't, because Cloudflare would silently skip them.
- **Still to verify before December:** that forwarding one message to *all* members works within
  the free plan's per-run limits. Test news@ with a realistic number of recipients and check the
  Worker's log says `forwarded to N/N`.
- Phase 2 (adding members automatically) will edit these variables. That needs Workers Scripts:
  Edit and must update one variable without disturbing the others.
