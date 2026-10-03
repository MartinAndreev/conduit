---
name: tui-visual-audit
description: Compare actual Conduit TUI captures with reviewed screen references and report visual defects after UI implementation. Use for visual review, not mockup generation or proof of business behavior.
---

# TUI visual audit

Advisory review in the conversation language. Follow repository instructions. Do not change code, approve a feature, or replace a reference to make a difference disappear.

## Establish comparable evidence

Read the relevant feature contract and screen gallery. Current galleries: `specs/004-project-navigation/screens/README.md` and `specs/003-session-workflow/screens/README.md` (paths relative to repo root). Identify exact reference file, its approval status, scenario, target viewport and any accepted departures. Draft concept images support exploratory feedback, not conformance verdicts. If the baseline or intended behavior conflicts with the spec, report the question instead of inventing a requirement.

Obtain an actual app capture for the same state. Record revision/dirty state, launch command, fixture, terminal columns/rows, font/zoom/theme, OS/terminal, keyboard focus and steps to reproduce. Use synthetic project data and fake harnesses; review does not authorize real model calls, credentials or modification of user projects. Prefer the existing repo capture/test workflow when available. A new executable capture helper is implementation and follows Sol delegation rules.

Inspect both images with available image-viewing tools. Capture may use supported computer-use tooling or a real terminal renderer. Plain stdout/ANSI logs help diagnose but do not alone establish rendered appearance. Never use image generation to simulate actual output. If no runnable screen or capture is available, report comparison as not performed; ask for the missing evidence when needed. Do not use voice-only screen tools outside an active voice chat.

## Compare meaning and layout

Check panel hierarchy and proportions, clipping/overlap/wrapping, borders and alignment, text legibility, Cyrillic/Unicode cell width, contrast, active focus versus selection, selected project/idea, status visibility, and available keyboard hints. Compare dynamic content using equivalent fixtures; dates, counts, cursor blink and terminal chrome are not automatically defects.

Generated references may contain inconsistent fonts, shading or glyphs. Judge feasible terminal cell layout and explicitly agreed design, not literal imitation of these artifacts. Do not silently normalize away clipping or hide a mismatch. Pixel differences are supporting evidence, not a sole verdict; do not invent thresholds or compliance percentages.

When executable UI exists, inspect relevant wide and narrow sizes, resize behavior, empty/error/long-content states and keyboard focus transitions within the authorized scenario. Distinguish a static visual finding from an interaction actually reproduced. Missing reference variants are design questions; obvious overlapping or inaccessible controls can still be evidenced defects.

## Report and handoff

Report each finding with severity by user impact, screen/state, expected versus observed behavior, reproducible location (panel or terminal row/column), links to both reference and actual capture, and a recommended correction. Separate defects from design suggestions and unresolved baseline questions. Save disposable captures/report in the agreed feature evidence location if requested; keep secrets and unrelated desktop content out.

Summarize coverage, untested states and environmental differences. No findings does not prove behavior or accessibility correctness. Recheck the affected screen and focus/resize states after an authorized correction; retain the earlier evidence rather than overwriting the baseline. Existing implementation-audit remains responsible for code/contract review; this skill supplies visual evidence for the human handoff.
