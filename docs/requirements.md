# Requirements baseline

The attached `too-broke-to-die-pdd-linux.md` (PDD) is the authority; section and scenario references below refer to it. This index covers the complete game, not just a slice. PDD §13 contains the individual requirements and acceptance wording; grouping here does not relax any of them. The task authorizes implementation from `main` despite the PDD's document-only status; it does not authorize release or other out-of-scope external actions.

## Complete-game contract

- One authored, offline, single-player 2D desktop game, Go and raylib, Linux x86-64 and Windows x86-64; Linux is the development and automated-check platform and Windows is cross-compiled. No account, service, updater, or network is required during play. Russian is the primary language of dialogue, UI, signs, and documents (PDD §§1, 10.1–10.2).
- The playable prologue establishes Grisha's theft of cancer-treatment donations, heart attack, funeral resurrection and Vera's subsidy bargain. The acts proceed through the hospital, church, ministry and institute, with Potroshin, Savva, Blagoev and Viralsky respectively; after the institute, return to the ministry (PDD §§4–6, 8.2, 8.6–8.7).
- Exactly 96 authored service tickets (four institutions × four families × six distinct instances) and four closure deeds yield exactly 100 distinct credits. Each institution contributes 25. The first three closures unlock the next act; the institute reaches 99 before its boss and `D-INS-END` is credit 100. Boss kills, repeated tickets, refills, combat and ending scenes grant no extra credit. Credit derives from durable distinct receipts, not a mutable balance or moral score (PDD §§7.9, 8.3–8.5, 8.9–8.10).
- At 100 credits and NAR-09, the ministry issues the permit without repayment or another deed. Consent can be cancelled; once committed, authorized death leads to Satan's rejection, appointment as health minister, and the casino ceremony. The mandatory acknowledged tableau combines the ribbon, four antagonists, Nina's evacuation, the burning children's hospital and the exact closing text. No alternate redemption ending replaces it (PDD §§6.9–6.12, 8.9).
- Full controls, remapping, keyboard/mouse and Xbox-layout controller, aiming, platforming, combat, four recoverable limbs, renewable feces, interaction and physical comedy remain required. Evidence, certification, harm and debt are separately communicated; failed attempts, hostility and catastrophes cannot permanently exhaust an uncertified source or block the ending (PDD §7, §§8.3–8.8).
- Checkpoints and restoration preserve committed receipts, incidents, milestones, boss victories and safe anchors while resetting transient excursion work. Stable event identities, atomic ordered transitions and two independently validated revisions prevent duplicate credit or charges and mixed recovery. Failure, corruption and unknown loss must be reported honestly; never fabricate receipts (PDD §§7.12–7.14, 10.8–10.9).
- Original final art, animation, audio and complete Russian presentation, readable HUD and tutorials, captions, remapping and other accessibility options remain product requirements. Final assets and provenance are external dependencies, not permanently replaceable placeholders. See [production dependencies](production-dependencies.md) and PDD §9.

## Requirement-to-evidence index

These are coverage targets, not passed results. PDD §12.2 maps each SYS-T, CAM-T and PRE-T scenario to its integrated FGA cases; its parameter sets remain mandatory. [Verification](verification.md) records current status and the candidate evidence required.

| PDD §13 requirements | Scope and PDD authority | Scenario families / audit |
|---|---|---|
| 1–3, 58–62 | Offline product, authorization distinction, Russian, platforms, builds and distributions (§§1, 10.1–10.2) | FGA-B01–B09; FGA-V05; AUD-08, AUD-10 |
| 4–15 | Premise, characters, institutions, adult tone and canonical story (§§2–6) | CAM-T01, CAM-T16–17; FGA-N01–N07; AUD-01, AUD-09 |
| 16–21 | Input, movement, combat, limbs, feces and physical comedy (§§7.2–7.8) | SYS-T01–T05, SYS-T11; FGA-G01–G07, FGA-G10; FGA-E03 |
| 22–29 | Certification, consequences, feedback, state, persistence and ending eligibility (§§7.9–7.14) | SYS-T06–T14; FGA-C03–C08, FGA-R01–R11, FGA-V04; AUD-03–04 |
| 30–40 | Acts, bosses, 100 sources, services, aftermath and ending gates (§8) | CAM-T01–T17; FGA-C01–C08, FGA-G08–G09, FGA-N03–N06; AUD-01–03, AUD-06, AUD-09 |
| 41–57 | Original art, animation, HUD, Russian, accessibility, audio, media handoff and rights (§9) | PRE-T01–T18; FGA-V01–V08, FGA-N06–N07, FGA-B04–B06; AUD-07 |
| 63–70 | Architecture, boundaries, Go conventions, content validation and deterministic simulation (§§10.3–10.7) | FGA-E01–E05, FGA-G01–G10; AUD-05 |
| 71–76 | Save schema, ownership, idempotency and honest recovery (§§10.8–10.9) | FGA-R01–R11, FGA-E04; AUD-03–04, AUD-06 |
| 77–82 | Main-thread native ownership, lifecycle, failures and local observability (§§10.10–10.12) | FGA-E06–E10, FGA-B05–B08, FGA-R06, FGA-R11 |
| 83–96 | Phase gates, all scenario matrices, full runs, risks and readiness separation (§§11–12) | PH-00–PH-09; SYS-T01–T14, CAM-T01–T17, PRE-T01–T18; FGA-N01–N07, G01–G10, C01–C08, R01–R11, V01–V08, B01–B09, P01–P05, E01–E10; AUD-01–10 |

Each shortened FGA range in the last row retains the `FGA-` prefix. All applicable requirements, parameter sets and specialist reviews are needed for full-game acceptance; a scenario definition, source check or phase demonstration alone is not evidence of completion.
