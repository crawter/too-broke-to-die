# Production dependencies and risks

PDD §§9.10, 10.2.3, 11.4 and 14 define the handoff and risk authority. No final binary media or rights evidence accompanies the supplied material. Availability, funding, schedule, providers and named owners are unknown; PDD role IDs describe responsibilities, not assignments. Implementation can proceed without this handoff, but final presentation and full-game qualification cannot be accepted without it. Publication, release, signing, storefront submission and spending on external providers are outside this task.

## Final handoff required

| Work | Required production coverage and evidence | Status |
|---|---|---|
| Art and animation | Eight principal character sheets and portraits, minister costume, eight enemy sets, 24 named RES adult appearances, four crowd kits, 16 dressed bays, four boss arenas, three prologue spaces, four ending packages and four transit stops. Include shared objects/finishes, 48 emergency-state packages, effects, HUD, menus, ledger and recovery UI; player, enemy, adult and boss animations with timing and socket markers. | Blocked: final exports and source handoff not supplied. |
| Audio and performance | 18 music cues, eight stingers, six ambience beds, all specified movement, body, service, warning, boss, UI and ending effects, principal vocal reactions and original institute trend performances. Include masters, runtime exports, clean loops, markers and mix evidence. | Blocked: recordings, masters and exports not supplied. |
| Fonts and Russian text | Cyrillic-capable Noto Sans files and applicable licenses; complete Russian dialogue, UI, signs, tutorials and captions with stable IDs and glyph coverage. The full text catalogue and editorial review remain work to be done. | Blocked for final font handoff; text and review not yet delivered or verified. |
| Provenance and rights | For every source and contribution record authorship, source material, licenses, permissions, performer permissions, attribution and unresolved concerns; review original expression against tonal references. Preserve notices for actual dependencies, fonts and assets. This review is not a claim of legal clearance. | Blocked: no source-specific rights records or permissions supplied. |
| Manifest and integration | Expand and bind all required assets and text to ENC, OBJ, DeedID, CON, EM, ANC, NAR and END identities. Record source/export paths, dimensions, pivots, animation markers, audio properties and actual file digests from real exports; validate all references. | Not run; actual final files and their digests are unavailable. |

Technical deliverables include editable layers and previews, sRGB RGBA PNG atlases (maximum 2048 × 2048 with specified padding), source art at twice logical resolution, and 48 kHz audio masters and runtime formats per PDD §9.10.2. Contact sheets and boss timing previews are required. Temporary blocks, silent slots, missing glyphs and unlicensed samples cannot satisfy final acceptance; a temporary audio-device fallback is not a substitute for missing audio files. No hashes or permissions are inferred here.

## Risk register index

PDD §11.4 supplies mitigations and gate responsibilities. These risks remain open until backed by evidence and reviewed disposition; no individual owner is assigned here.

| PDD risk | Exposure / next evidence |
|---|---|
| RISK-01, RISK-10 | Missing binary media and provenance; representative final assets for PH-02 and complete handoff, originality and rights review for PH-08/09. |
| RISK-02 | Dependency selection can break Linux or Windows builds; run the source gate and review actual selected licenses. |
| RISK-03 | Wrong-thread native calls or unsafe cleanup; ownership and lifecycle tests, then desktop verification. |
| RISK-04, RISK-14 | Interrupted or lost saves and physical media loss; fault-inject commit boundaries, retain valid revisions, document limits and never invent receipts. |
| RISK-05, RISK-09 | Repetitive families or misdirected satire; play and native Russian editorial review across the full campaign. |
| RISK-06, RISK-07 | Effects hide warnings, or aftermath blocks a mandatory route; audit readability, anchors, actors and recovery under all applicable variants. |
| RISK-08 | Final media exceeds resource budgets; measure final loads, memory and endurance. |
| RISK-11 | Driver/device differences and unexecuted Windows runtime; manual desktop, device and Windows smoke qualification. |
| RISK-12 | Signing, storefront, rating and jurisdiction decisions belong to separate authorized release work, not this implementation task. |
| RISK-13 | Header uncertainty addressed by the person's exact repository convention in [README](../README.md#source-headers); application and compliance still require source review. |

A risk mitigation is not evidence of a pass. PDD §12.11 AUD-07 requires actual final asset, owner and provenance records; PH-09 needs a disposition for every risk and cannot treat unresolved requirements as waived.
