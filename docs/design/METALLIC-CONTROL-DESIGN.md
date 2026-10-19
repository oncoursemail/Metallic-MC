# Metallic Control — design direction v2
Status: DESIGN SPECIFICATION, not a finished UI. Applies to Metallic MC management and future shared infrastructure control.

## Design thesis
"Precision hardware instrumentation meets a premium game studio tool." Build a credible professional workstation, NOT the default AI SaaS template. Avoid excessive rounded cards, arbitrary neon gradients/glow, glassmorphism, glowing purple borders, stock icons, emoji badges, gigantic KPIs without provenance, fake sparklines, marketing-style sidebar menus or the same four-column grid on every page.

## Information architecture
**Fleet:** real-time cluster topology, deployment health, active instances, player sessions, events.
**Operations:** incident timeline, alerts, player-facing impact, trace-to-tick view, backups and audit logs.
**Compute:** CPU cores, affinity, actual memory/PSI pressure, physical host topology, queue admission and reservations.
**Game services:** Minecraft Paper/Folia/Minestom, Metallic sessions, world templates, auto-start plans, plugin quality.
**AI Lab:** Star AI tasks, observed learning/inference resources, experimental performance patches, approval workflow.
**Old Servers:** clearly separated legacy Wings inventory, migration comparison, read-only default.

## Three signature screens
1. **Operations overview:** Left compact functional navigation; center the physical cluster topology and incident queue; right contextual inspector of selected machine/service. Show 1/5/15-min measurements as time-series only where historical data exists; never invent trends.
2. **Workload inspector:** Dense timeline, process/cgroup telemetry, perf trace annotations, reservations, restart history, plugin call flamegraph entry points. A game instance should reveal player count and tick latency, not only "CPU %" and "Online."
3. **Match fleet:** A live occupancy/availability matrix segmented by Java/Bedrock, game type, region and state (Ready, Allocated, Draining, Failed). Servers clearly show cold/ready transitions. Avoid fake global capacity totals.

## Visual system
- Neutral deep graphite, three neutral elevations, differentiated by subtle hairline contrast, not shadows; one restrained proprietary Metallic accent. Accents signify selection/actions, not decoration.
- Typography: self-hosted appropriately licensed UI sans, strong tabular numeric data, compact labels, visibly different heading and metadata hierarchy. No oversized center-aligned hero on an admin screen.
- 4/8px baseline rhythm; dense tables, first-class keyboard navigation, accessible hit areas, clear selected/focus states, WCAG 2.2 AA contrast.
- Prefer bespoke graph geometry, diagram legends and incident typography over random illustrated decorative art.
- Animation: meaningful 120–220ms transitions, stable instrument gauges, connection/reconciliation state indication; obey reduced-motion, never animate counters without signal integrity; no transitions that obscure an alert.
- Color denotes health only alongside text/shape. Display timestamp, freshness, data source and units with every measured chart.
- Keep zero-data/loading/error/stale/conflicted/permission-denied/partial-cluster states designed, not generic spinners.

## Interaction prototypes required before implementation
A. Desktop 1440px fleet overview: healthy + multi-node degraded states.
B. 1280px detailed workload inspector with dense real telemetry.
C. 390px mobile **operations emergency panel**, not shrunk desktop tables.
D. Keyboard-only flow: locate node -> trace incident -> approve safe action -> see audit.
E. Accessibility checks: contrast, focus and reduced motion.

## Anti-slop review gate
- Could one replace METALLIC with any random SaaS name and nothing would change? Reject.
- Are cards repeated without clear decision purpose? Reject.
- Do real operators see urgent incidents, uncertainty and data age in under 5 seconds? If not, revise.
- Do diagrams prioritize real relationships over decorations? If not, revise.
- Does every control have a grounded real state and permission? If not, disable and explain.
- Conduct blind qualitative usability critique with operator tasks, not 'looks modern' scoring only.

## Assets
Create unique visual identity and original graph/icon families through design exploration, vector sketches and human critique. No direct copying of Mineclub, Linear or competitor assets. UI imagery must never pretend a rendered concept is a live deployed screenshot.

## Acceptance tests
Functional Playwright visual/regression tests, screenshots at 390/768/1280/1440, keyboard and screen-reader smoke checks, reduced-motion, Lighthouse accessibility as secondary metric. Require actual user approval before calling a visual direction finished.
