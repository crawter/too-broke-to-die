# Manual dependencies and qualification

## Boundary and current status

Implementation of the game, source and text is authorized. Commissioning,
purchasing, signing, deployment, publication and release are separate actions
requiring separate authorization. Nothing here commissions a provider,
purchases a license, assigns a person, signs an artifact or authorizes a
release. Provision of final binary media and rights evidence is a dependency,
not an assertion that external work has been performed.

The PDD requires the complete offline Windows 11 x86-64 game, not the
representative slice alone. PH-02 requires representative **final-quality**
character, bay, UI, music and effects as well as integrated mechanics and
pipeline evidence. PH-08 requires the complete final-media handoff; PH-09
requires qualification of the same candidate. Missing media or evidence does
not prevent all source work, but it prevents the affected phase and
complete-game gates from passing.

`media-handoff.md` is the inventory and PRE index.
`rights-ledger.md` records pending provenance.
`implementation-plan.md` defines the stacked phase sequence and risks;
`rule-compliance.md` defines the baseline evidence rules.

## Required external provision and review

| Dependency | Responsible role | Required handoff or observation | Present status |
|---|---|---|---|
| Final visual art, animation and editable sources | OWN-PROD coordinates; OWN-ART reviews | All §9 groups, exports, source layers, contact sheets, collision previews, event markers, sockets, accessibility variants and boss timing previews. | **Blocked:** no final delivery or provider confirmed. |
| Final music, effects, ambience and performances | OWN-PROD; OWN-AUD | 18 cues, 8 stingers, 6 ambience beds, all effects and used vocal sets, editable sessions, masters, loop points, captions and mix evidence. | **Blocked:** delivery and performers unconfirmed. |
| Fonts, applicable licenses and notices | OWN-PROD; OWN-RIGHTS and OWN-PLAT | Actual Noto Sans files, full glyph coverage, source/version, applicable license and attribution; inspect actual dependencies and decoder notices too. | **Blocked:** files and rights evidence unconfirmed. |
| Authorship, source, license and performer evidence | OWN-RIGHTS | Complete per-contribution ledger and permissions for intended offline packaging and use, source-material and sample records, notice obligations and unresolved concerns. | **Blocked:** evidence not supplied. |
| Original-expression review | OWN-ART, OWN-AUD and OWN-RIGHTS; QA records | Compare final characters, scenes, layouts, music and source materials against tonal references; reject recognizable copied expression and undocumented samples. | **Blocked:** final material and reviewer unconfirmed. Review is not legal clearance. |
| Native Russian editorial review | OWN-RU; QA records | Review complete script, rendered interface, captions, names, variables, ё, tone, exact inherited lines and ending against §§6 and 9. | **Blocked:** complete candidate and reviewer unconfirmed. |
| Final visual and audio review | OWN-ART, OWN-AUD, OWN-QA | Candidate captures, all animation events, accessibility variants, loop listening, loudness/peak measurements and physical playback devices. | **Blocked:** final candidate, media and environments unconfirmed. |

The complete Russian source catalogue is authorized implementation work.
Its editorial review and any recorded performances still require suitable
reviewers and production capability. Temporary media can support development
and isolated tests only; it cannot substitute for final exports or rights.

## Physical environments and people

OWN-PROD must arrange access; OWN-PLAT prepares the candidate and OWN-QA
records execution and independent review where staffing permits. Availability
is unconfirmed; do not claim any environment passed.

| Environment or capability | Required use |
|---|---|
| `ENV-REF`: Windows 11 24H2 with supported updates, Intel Core i5-8250U, Intel UHD 620, 8 GiB RAM and SSD | Full campaign, performance, persistence and lifecycle qualification, including final media. Equivalent hardware does not replace this reference performance evidence. |
| `ENV-AMD`: Windows 11 x86-64, Ryzen 5 5600G integrated graphics, ≥8 GiB RAM, SSD | Startup, all boss phases, emergency, display, audio, save and ending compatibility. |
| `ENV-NVIDIA`: Windows 11 x86-64, GTX 1650 or equivalent newer supported GPU, ≥8 GiB RAM, SSD | Same compatibility coverage, high refresh and device transitions. |
| `ENV-CLEAN`: clean standard-user Windows 11 x86-64 or equivalent physical test image | Offline ZIP operation without development tools, compiler, raylib install, account or network. A VM may support clean-package checks, not replace physical graphics, audio or performance evidence. |
| `ENV-UPDATED`: latest supported Windows 11 x86-64 update available at qualification | Clean startup and complete compatibility scenario set. Record exact edition, update, hardware and driver. |
| Wired Xbox-layout controller; keyboard and mouse; working, missing and disconnectable audio; laptop speakers, headphones and mono output | Real input, remapping, focus, disconnect/reconnect, sound-device failure, listening and accessibility cases. |
| Supported local fixed NTFS storage and physical interruption/power-loss setup | Persistence, recovery, confirmed-progress and power-loss evidence. Fault-injection tests do not prove the physical boundary. |
| Native Russian editor, visual/animation reviewer, audio reviewer, originality/rights reviewer and QA executor | Full-script, rendered-layout, listening, comparative originality and candidate qualification records. A role is not an assigned person. |

One physical machine may satisfy multiple environment IDs if its configuration
is recorded. Record any allowed equivalent-hardware rationale; retain all
required parameter sets. A target build, device access or test fixture is not
itself a passing result.

## Three real fresh-profile campaigns

On the same final packaged candidate, OWN-QA records three complete runs from
fresh profiles to ST-END, without fixture selectors, edited credits or
shortcuts:

1. Keyboard and mouse.
2. Controller.
3. A run including failed attempts, catastrophes, interruptions, revisits and
   high debt.

The third run must actually observe those conditions, not merely claim they
were possible. Each run records the full sorted receipt identity set, closures,
99-to-100 transition and ending evidence; a screenshot of `100/100` is
insufficient. Automated simulations and isolated encounters do not count as
one of the three. Each run is **blocked** until an appropriate packaged
candidate, final content, environment and executor exist; the scenario is
**not run** until actually attempted.

## Candidate-bound qualification

For each scenario **and parameter set**, retain the PDD §12.1 record:
requirement/scenario IDs, source revision, executable hash, content digest,
Windows edition/build/update, hardware and drivers, setup, device, settings,
fixture identity if applicable, procedure, expected and observed results,
evidence files, status, defect references, executor, reviewer and date.
Generated identities and checksums must come from checked build/content
commands, not handwritten guesses. A changed source, dependency, content,
build or environment invalidates affected results and requires rerun and
review; do not transfer a pass by assertion.

Full-game readiness requires all §13 requirements; all SYS-T01–T14,
CAM-T01–T17, PRE-T01–T18, and FGA-N01–N07, G01–G10, C01–C08,
R01–R11, V01–V08, B01–B08, P01–P05 and E01–E10 parameter sets; the
three campaigns; final assets and rights records; performance, package,
compatibility and lifecycle evidence for the same candidate; no unresolved
blocker or major defect; and no minor defect violating an acceptance
condition. The PDD supplies the detailed procedures and thresholds.

In particular, ENV-REF performance uses final content, default audio, full
blood particles and 100% text at 1,920 × 1,080/60 Hz. Preserve raw
measurements, workload parameters and exclusions under §12.9. Do not remove
effects, warnings or required content to obtain a result. Native Windows cgo,
clean-machine, physical-device and power-loss checks are not replaced by
portable source tests.

OWN-PROD records candidate readiness only after reviewing the evidence and
risk dispositions. Readiness is **not** a decision to sign, deploy, publish,
submit for a rating or release. Those actions need separate authorization,
owners and their own applicable checks; signed artifacts require separate
verification and final hashes. The 18+ notice is not a formal age rating.

## Status vocabulary and baseline

- **Passed:** executed against identified inputs and independently reviewed
  where required; observed results satisfy the stated parameter set.
- **Failed:** executed and an observed result violates a criterion; record
  defect and evidence rather than relabeling it blocked.
- **Blocked:** a required prerequisite, environment, permission or person is
  unavailable; record the dependency and owner.
- **Not run:** no execution has occurred, whether or not its prerequisites
  will eventually be available.

| Evidence | Status now |
|---|---|
| Documentation inventory, count, traceability and status checks defined below | **Not run**; these instructions are not their result. |
| Project source, build, automated and native checks | **Not run**; the inspected tree supplies no module, implementation or runnable project check command. |
| Final media, provenance, permissions and specialist review | **Blocked**; actual deliveries and evidence are not supplied. |
| Target environments, devices, physical power-loss checks and three campaigns | **Blocked**; candidate and access are unconfirmed. Individual scenarios remain **not run**. |
| Failed or passed checks in this handoff | None observed. This is not a claim that checks were executed successfully. |
| Candidate readiness and release | **Blocked**; required evidence is absent. Release additionally requires separate authorization. |

## Documentation checks for the lane

Reviewers should record command or procedure, input revision, executor,
reviewer, date, observation and one of the four statuses above. These are
required checks, not claimed results:

1. Compare `media-handoff.md` with every §9.10 group and §9.7 audio item.
   Recount 8 principal sheets and costume, 8 enemy sets, 24 named adults,
   4 crowds, 16 bays, 4 arenas, 3 prologue packages, 4 transit stops,
   16 emergencies × 3 states, 4 ending packages, 18 cues, 8 stingers and
   6 ambience beds. Check the specified animation and tutorial expansions.
2. Expand the explicit institution, family, number, state and attack sets.
   Check all 96 service IDs, 4 closures, 100 consequences, 16 emergencies,
   20 institutional encounters, prologue, anchors, narrative and ending
   identities against PDD §8. Check each applicable object role and
   24 RES names against §8.5. Reject gaps, duplicate uses presented as new
   deeds and references to invented completion evidence.
3. Check every group has quantity or a clearly defined variable cardinality,
   campaign binding, editable source, runtime export, technical constraint,
   responsible role and review evidence. Check pivots, sockets, markers,
   interruption points, loop boundaries, caption IDs, glyph coverage and
   accessibility variants explicitly.
4. Cross-check every PRE-T01–PRE-T18 row with PDD §9.11 and the integrated
   owner and FGA mapping in §12.2. Verify that each parameter is retained,
   including all bosses, emergencies, text sizes and minimum-size tableau.
5. Compare rights-ledger coverage against every inventory group, actual
   individual contribution and third-party component. Pending is not
   clearance. Check authorship, source materials, licenses, performers,
   attribution and comparative originality independently.
6. Check all status statements against observed evidence and the external
   authorization boundary. A missing prerequisite remains blocked; an
   unattempted scenario remains not run. No placeholder, silent slot or
   fixture counts as final acceptance.

No runnable project check is defined in the current inspected tree. The lane
must run any available project checks if introduced, and record exact
commands, environment and results. The PDD proposes `gofmt -l`,
`go vet ./...`, `go test ./...` and `go test -race ./...` once a module and
supported toolchain exist; native Windows cgo, build, package and physical
checks have separate prerequisites. These proposed commands are not evidence
that a check ran. Unsupported or unavailable checks must not be reported as
passed.
