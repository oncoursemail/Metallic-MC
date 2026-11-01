# Metallic Control Surface — art direction and product-quality specification

**Status:** original working UI implementation (source prototype), not approved final branding. The owner rejected the previous v0.1 preview. This v0.2 direction is deliberately different and requires owner critique and iteration.

## Visual language: precise industrial editorial

- An operations **instrument**, not a template-driven SaaS landing page. Light off-white working canvas, midnight mechanical side rail, warm copper signal. No gradient blobs, blurred glass, oversized round cards, default Lucide-card stacks or purposeless 3-D art.
- The UI is the product's data: honest connectivity status, error/failure states, real compute summaries, workload admission decisions. Fake graphs/data are forbidden in production; marketing-style screenshot fixtures must be labeled **illustrative**.
- Brand mark is an original 2-prong geometric *placeholder* made in vector markup; not an approved Metallic identity. Replace only after dedicated brand work.
- Typography: system-backed Clear Sans/Inter family with monospace machine identifiers; high contrast and restrained numerical tracking. No externally hosted fonts or analytics.
- Consistent 1px boundaries, square controls, 8px spacing rhythm, no arbitrary eye-catching shadows. Tight data-dense tables; high hierarchy from editorial headline and instrument panels.
- Signal colors only for operational health; copper is navigational and art-directing rather than falsely signaling a warning.
- Responsive 360px–1600px; keyboard/focus indicators, semantic buttons, reduced-motion support, native selects, status text with dot (never color alone).
- Empty states are deliberately factual and provide a path to configure agent telemetry; no placeholder values deceptively shown as actual servers.

## Design quality gates

1. Owner review **first**, using desktop and mobile screenshots of the *actual source-built dashboard*; do not call a concept final without approval.
2. Accessibility: WCAG 2.2 AA for key screens, keyboard-only navigation, automated accessibility audit plus manual tab-order and mobile touch-target checks. Contrast 4.5:1 for ordinary text and 3:1 for UI boundaries/large text.
3. Pixel QA at 1440×900, 1366×768, 1024×768, 390×844 and 320×568. Screenshots after real browser render, not image-generation substitutes for code.
4. No fictional KPIs; verified API data only. Preview fixtures must be watermarked **design fixture — not live**. Add realistic edge cases: long hostnames, offline nodes, stale data, empty fleet, high load, unknown I/O PSI.
5. Interaction QA: nav/panel routing, signed-in session state, logout, drop-down selection, security errors, sync polling, mobile horizontal navigation and no console exceptions.
6. Performance: CSS/JS static and local; avoid external network font/CDN dependencies. Track first contentful paint and total asset size, use progressive complexity only when useful.

## Grounding

- IBM Carbon accessibility and data table guidance: https://www.carbondesignsystem.com/building-blocks/core/components/data-table/accessibility/ and https://www.carbondesignsystem.com/building-blocks/core/components/data-table/guidelines/
- Grafana SAGA and Carbon empty states: https://grafana.com/developers/saga/patterns/empty-state/ and https://www.carbondesignsystem.com/building-blocks/core/patterns/empty-states
- WCAG 2.2: https://www.w3.org/TR/WCAG22/

The design is **original** and borrows principles, not screenshots, intellectual property or proprietary assets.
