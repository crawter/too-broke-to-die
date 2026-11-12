# Verification and evidence

PDD §§7.15, 8.11, 9.11 and 12–13 define the scenarios, parameters and complete-game gate. This record separates implementation checks, external production and manual qualification. Definitions are not executions. No source, build, distribution, game run, asset or qualification success is asserted by this baseline.

## Status and evidence rules

- **Pass**: the specified procedure and every required parameter set ran on the identified candidate and met the expected result, with reviewable evidence.
- **Fail**: execution produced a result that violates the specified condition; link the defect and affected candidate.
- **Blocked**: a prerequisite needed to execute or accept the case is missing; identify it. Blocked is not Pass.
- **Not run**: no execution evidence exists, including manual work not yet performed. Not run is not Pass.

For each scenario and parameter set record its ID and requirement references, source revision, executable hash and content digest; OS, distribution, driver and hardware; setup, device, settings, fixture and steps; expected and observed results; logs, screenshots, recordings, assertions or measurements; status, defect, executor, reviewer and date (PDD §12.1). Do not invent values. Ranges below include every intervening ID, and PDD §12.2 supplies the mapping and parameters for individual SYS-T, CAM-T and PRE-T cases.

## Current evidence by track

| Track and trace | Status | Remaining evidence / boundary |
|---|---|---|
| Bootstrap documentation | Not run in this record; the lane must report the actual result. | `git diff --check` is the bootstrap gate. No Go module or `scripts/check.sh` exists in the inspected baseline. |
| Implementation/source gate; FGA-E01–E10 and applicable automated FGA-N/G/C/R/V cases | Not run for game implementation | The dependent source-gates change introduces `scripts/check.sh`: module tidy, formatting, Linux vet/build/tests, race-sensitive coverage, and Windows vet/build with `CGO_ENABLED=0`, all on Linux without opening native devices. See PDD §§10.2.2, 12.10. Bootstrap does not invoke the nonexistent script. |
| Story and campaign: SYS-T01–T14, CAM-T01–T17; FGA-N01–N07, FGA-G01–G10, FGA-C01–C08 | Not run | Exercise all tickets, four bosses and phases, 16 emergencies, exact 0–100 receipt arithmetic, repeat/failure paths, high debt and full ending. PDD §§12.2–12.5 define parameter sets. |
| Persistence: SYS-T07–T14, CAM-T10–T17; FGA-R01–R11 | Not run | Test same-tick ordering, idempotent retries, both valid revisions, corruption, failed writes, locks, consent and ending recovery. FGA-R05 repeats each specified write cut point 20 times. Fault injection is not physical power-loss proof. |
| Production and presentation: PRE-T01–T18; FGA-V01–V08 | Blocked for final-media/provenance acceptance; other unexecuted cases Not run | No final exports or rights evidence supplied. Validate actual manifest bindings, source handoff and rights, then review original expression, animation, Russian, audio, accessibility and final tableau. Placeholders and silent slots do not pass final presentation. |
| Build and compatibility: FGA-B01–B09 | Not run | Automated Linux and Windows cross-build checks are distinct from candidate archives, offline desktop use and Windows runtime smoke. Packaging implementation or a cross-build alone is not an accepted distribution. |
| Manual qualification: FGA-N01–N07, G02/G08/G10, V01–V08, B02–B09, P01–P05 and applicable E cases | Not run | Requires the specified desktop, displays, physical devices where applicable, people and final candidate. Includes focus and controller changes, audio/graphics behavior, Windows smoke, perceptual Russian/media review, controlled physical power-loss exercise when safe equipment is available, and performance/endurance. These do not gate implementation steps. |
| Whole candidate: AUD-01–AUD-10; PH-09 | Blocked | No implementation, final media, rights evidence, candidate or executed full-game qualification has been established. See [phases](phases.md) and [production dependencies](production-dependencies.md). |

Where a case spans automated and manual evidence, a passing display-free portion does not pass its remaining device or perceptual parameters. ENV-CHECK is Linux x86-64 with Go, a C compiler and X11/OpenGL/Wayland development headers; no display is needed. ENV-DESKTOP and ENV-WIN are manual qualification environments (PDD §12.8).

## Full-game completion gate

PDD §12.1 requires all §13 requirements and all SYS-T, CAM-T, PRE-T and FGA scenarios to pass their mandatory parameter sets. Three fresh-profile campaigns must reach ST-END without fixture shortcuts: one using keyboard and mouse, one a controller, and a third covering failures, catastrophes, interruptions, revisits and high debt. All final media and required rights records must be present; performance, packaging, compatibility and lifecycle evidence must identify the same candidate. No blocker or major defect may remain, and minor defects may not violate acceptance. Production must record readiness separately from any release decision.

Manual performance uses final media and the PDD §12.9 workloads, reference environment and thresholds, not a source-check proxy. Source checks, unrun scenarios, a vertical slice, temporary media or a cross-compiled executable cannot by themselves close a phase or establish full-game readiness. Publication and release remain outside this task.
