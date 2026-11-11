# Rights and provenance ledger

## Status and scope

This is an initialized register of **pending evidence**, not a record of
completed production, permission, licensing, originality review or legal
clearance. No authors, performers, providers, source files, license versions,
rights holders or reviewers have been identified for the final media. OWN-RIGHTS
owns the review; OWN-PROD coordinates missing evidence. Role names are not
assignments.

The PDD's tonal references are not licenses. The supplied funeral and Hell
exchanges and closing line are narrative anchors of the game; their presence
in the specification is not evidence that every future contributor or
third-party source has granted rights. The selected Go copyright header is
not a substitute for a media or dependency rights record.

`media-handoff.md` defines the required group and binding expansions.
A group row below is an intake placeholder, **not** one blanket clearance for
all files. Before acceptance, expand it into one record per actual shipped
asset and relevant contribution, including shared sources, derivatives,
recordings and performers. Bind each record to every asset and campaign use it
covers. A missing, ambiguous or inapplicable record must be resolved
explicitly; silence is not approval.

## Evidence required for each contribution

Record, with evidence references and versions rather than unsupported claims:

1. Asset/contribution ID, source and export identity, version, campaign
   bindings, contributor and actual role; identify all authors, composers,
   arrangers, illustrators, editors and other creators as applicable.
2. Origin of every source element, reference, photograph, texture, font,
   recording, sample, tool-generated output and derivative. Record whether
   third-party material is present and retain the source and applicable
   terms; “original” alone does not document a source investigation.
3. Ownership or permission evidence for the actual intended uses: editing,
   adaptation, incorporation, offline packaging, distribution and other
   relevant uses. Record scope, territory, duration, restrictions and any
   conditions in the supplied instrument. Escalate uncertainty; do not infer
   rights from possession of a file.
4. License name, version, actual license text and source, applicable
   obligations and compatibility review for each third-party component.
   Distinguish a license from proof of authorship or performer consent.
5. Each performer and recording: permission for the actual recording and
   intended uses, including ensemble trend shouts and nonverbal takes.
   Document any sample, library, instrument or tool-output restrictions.
6. Attribution wording and destination, notice text and package path,
   including `licenses/` and `THIRD-PARTY-NOTICES.txt` when required.
   Verify notices against the exact files and versions shipped.
7. Comparative originality review of source and final expression against
   the PDD's references: character silhouettes, costumes, scenes, camera
   arrangements, levels, UI, dialogue, compositions and recordings. Record
   reviewer, date, comparisons, concerns, resolution and evidence. Replace
   or resolve recognizable copied expression and undocumented samples.
8. Implementer, reviewer, review date, evidence location, status, unresolved
   concerns, source revision and candidate content digest when integrated.
   Preserve history on replacement and invalidate affected reviews.

Rights review documents evidence and concerns; it is not a certification of
legal clearance. If specialist legal advice or another authorization is
needed, OWN-PROD must obtain it separately rather than recording an invented
approval.

## Pending media entries

Every row is **Pending / blocked: no actual delivery or evidence supplied**.
Each row covers all identifiers in the corresponding inventory group,
including every expanded use. The needed evidence is the full per-contribution
record above, not merely the examples in the last column.

| Intake group | Coverage to expand | Additional evidence to obtain |
|---|---|---|
| Principal character art and portraits | `ART-CHR-GRI`, `VER`, `POT`, `SAV`, `BLA`, `LEV`, `NIN`, `SAT`, including six expressions per sheet and GRI minister costume | Model sheets, layered sources, illustrator and reference history; comparative silhouette, costume and acting review. |
| Ordinary characters | All `ART-EN-I-{MEL,RNG}`, 24 `ART-RES-I-N`, 4 `ART-CROWD-I` and their source parts | Each artist and derivative part, adult portraits, reference sources and comparative review; no assumption that a shared modular part covers every contributor. |
| Environments and story art | Six ART-SET groups, 16 `ART-BAY-I-F`, 4 `ART-BOSS-I`, `ART-PRO-01–03`, 4 `ART-TRANSIT-I`, 4 `ART-END-*` | Sources for architecture, signage panels, palettes, tableau and all layers; reference and composition review, including the uncropped final tableau. |
| Objects, emergencies and effects | All `ART-OBJ-{family}-{role}` uses, 48 `ART-EM-I-F-{WARN,ACTIVE,AFTER}` packages and four FX families with reduced variants | Individual source elements and derivatives, source materials, object label bindings and originality of reusable effect elements. |
| Animation | All GRI, enemy, RES and boss clips, transitions and defeat clips in the handoff | Animator contributions, rigs, drawings, motion references, editable projects, event metadata and any third-party motion material. |
| Interface and original lettering | HUD, menu, ledger, recovery, markers, icons, glyph artwork, title and emblem lettering | Designer and lettering authorship; sources and terms of any icons, textures or tool output; focus/accessibility variants. |
| Fonts | Actual Noto Sans Regular, SemiBold and Bold files and any other shipped font material | Exact file versions, source, applicable license texts, bundling and attribution obligations; verify actual glyph coverage separately. No license is presumed cleared. |
| Music | All 18 `MUS-*` cues, stems, scores, transition tails and arrangements | Composer and arranger identities, source score, recording and performer permissions, sample and instrument-library provenance, actual license obligations and comparative music review. |
| Stingers and ambience | Eight `STG-*` and six `AMB-*` records, their masters and stems | Composition, recording and sound-library sources; permissions for any ambient voices or recordings; loop and derivative history. |
| Sound effects | `SFX-MOVE`, `BODY`, `LIMB`, `FEC`, `SERVICE`, `WARN`, all four `SFX-BOSS-I`, `SOURCE`, `UI`, `END` | Source for each take and variation, sound-library licenses, recording rights, processing and any tool-output conditions; no real injury recording for tearing. |
| Nonverbal and trend voices | Each used `VOC-{character}-{EFFORT,HURT,LAUGH,REACT}` category and `VOC-TRE-SIXSEVEN`, `LEVEL`, `BRAND` | Every performer, ensemble member and recording permission; evidence of newly recorded performances and no sampled meme or known-person voice imitation. |
| Russian text | All `TXT`, `DLG`, `TUT`, `CAP`, signs, receipts, script, documentation and named variable forms | Writers and editors, source history, contributor permissions and provenance of any external text. Record editorial review separately from rights review. |

The shorthand in this table refers to the exact full IDs and expansion sets
in `media-handoff.md`; it does not create new asset IDs.

## Pending dependency and package entries

These are separate from final-media authorship and must be checked against
the actual locked inputs and shipped files. Proposed pins and expected
permissive terms are not verified evidence.

| Intake group | Coverage | Status and required evidence |
|---|---|---|
| Go and native toolchain | Actual Go, GCC, MinGW-w64 compiler and runtime inputs | **Pending:** exact archives and versions, provenance, applicable licenses, runtime redistribution and notices; checked intake generates identities. |
| Graphics binding and native libraries | Actual raylib-go binding, bundled raylib and GLFW | **Pending:** source revisions, license texts, notices, modifications and dependency review. |
| Bundled decoders and other dependencies | Each decoder and additional shipped component, including any monitoring/error dependencies | **Pending:** actual versions, source, license texts and notices. No dependency is cleared merely by appearing in the plan. |
| Package notices | `licenses/`, `THIRD-PARTY-NOTICES.txt` and any asset-specific notices | **Pending:** compare actual package files and obligations against every resolved ledger entry. Do not fabricate notice text or checksums. |

## Review and acceptance procedure

1. OWN-PROD receives the delivered sources, exports and contributor evidence
   through an independently authorized provision process. Record the actual
   supplier and versions only when evidenced.
2. OWN-RIGHTS expands each relevant group row to individual records, checks
   authorship, sources, permissions, licenses, performers and attribution,
   and lists every unresolved condition. OWN-ART and OWN-AUD perform
   comparative original-expression review; OWN-RU reviews text sources and
   the full Russian script.
3. OWN-PLAT checks that the generated manifest and package enumerate the
   exact reviewed files, versions, licenses and notices. A changed file,
   source, license, permission scope or candidate invalidates affected
   records and requires renewed review.
4. OWN-QA records PRE-T01, PRE-T17, PRE-T18, FGA-V01 and FGA-V08
   observations for the same identified candidate. These do not replace the
   remaining presentation and full-game scenarios.
5. OWN-PROD keeps PH-00 intake, PH-02 representative media, PH-08 final
   presentation and PH-09 readiness blocked wherever their required records
   are missing. A reviewed source check or a development placeholder cannot
   close these gates.

For ledger entries, use **Pending** until evidence exists; then record the
actual review outcome and any defects. For qualification scenarios use
**Passed**, **Failed**, **Blocked** or **Not run** as defined in
`manual-dependencies.md`. No entry in this initial ledger is a passed rights
review. A readiness record is not authorization for signing, deployment,
publication or release.
