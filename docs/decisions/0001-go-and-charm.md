# 0001 — Build in Go with the Charm TUI libraries

- **Status:** Accepted
- **Date:** 2026-11-13

## Context
The tool must be a real, installable app that runs without cloning the repository, works for a
possibly less-technical future webmaster, and costs nothing.

## Decision
Write the tool in **Go**, using the [Charm](https://charm.land) libraries:
- **Bubble Tea** — the TUI framework (screens, keyboard handling).
- **Lip Gloss** — styling (colours, borders, layout).
- **Cobra** — the command-line commands (`aps-tools check`, `aps-tools doctor`, …).

## Alternatives considered
- **Python + Textual.** Pleasant to write, but users must have the right Python installed, and
  packaging is fiddlier. Harder to hand to a non-technical successor.
- **Node.js + Ink.** Same runtime-dependency problem as Python.
- **A web app.** Would need hosting (cost) and authentication (complexity).

## Consequences
- The app is a **single file** with no runtime to install. Easy to distribute and hard to break.
- A future maintainer needs basic Go knowledge to change the code. To offset this, the code is
  commented for readers who are new to Go, and [development.md](../development.md) explains
  the workflow step by step.
