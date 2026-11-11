# Final-media production handoff

## Status and authority

The attached PDD is the authority, especially §§6, 8, 9, 10.2.3 and
12.1–12.9. This register is a production brief, not evidence that a file,
producer, permission, or review exists. The phase sequence and candidate
evidence rules are in `implementation-plan.md` and `rule-compliance.md`.
External actions and qualification dependencies are in `manual-dependencies.md`;
pending provenance is in `rights-ledger.md`.

Final exports, editable sources, licenses, performer permissions, and
specialist reviews are **blocked: not supplied or not confirmed**. All
presentation scenarios are **not run**. Temporary art, test fixtures, silent
audio slots, missing glyphs, and unlicensed material cannot close PH-02,
PH-08, or complete-game acceptance. An explicitly chosen silent-play
fallback for an unavailable audio device is not an exception to the required
audio inventory.

Roles below are responsibilities, not assignments. OWN-PROD coordinates;
OWN-ART owns visual and animation review; OWN-AUD owns sound and mix review;
OWN-RU owns Russian writing and editorial review; OWN-RIGHTS owns provenance
review; OWN-GAME and OWN-PLAT integrate content and runtime behavior; OWN-QA
records qualification. Record the implementing and reviewing people separately.
No role holder or provider has been confirmed.

## How to use the register

The tables describe required production groups, not a claim that a group is
one physical file. Quantities are minimum required identities or uses, not
asset files or credit sources. Where a table says “one per” or gives a
Cartesian expansion, the production manifest must contain individual records
and explicit bindings for every member; do not leave a range as the only
runtime record. Shared art may serve multiple uses, but each use must resolve.

For every delivered record retain:

- Stable asset ID, version, source and export paths, source and export
  identities, responsible role, and provenance ledger reference.
- LOC, ENC, OBJ, DeedID, CON, EM, ANC, NAR, END, UI feedback, or other
  applicable bindings. Each required use must resolve without treating a
  filename or Russian text as a gameplay identity.
- Visual logical size, pivot, intended scale, layer and variant; animation
  duration, event markers, sockets and permitted interruption points; audio
  loudness, channels, loop/sample boundaries and caption identity.
- Editable source and runtime export, technical-validation results, review
  evidence, outstanding issues, and the candidate content digest when
  integrated. Generate expanded manifests and digests with checked commands;
  do not invent them.

All groups below require editable sources, final runtime exports, rights
evidence, technical inspection, and candidate-bound integration evidence.
Group-specific additions are listed in the tables. A successful file-format
check does not replace visual, listening, editorial, or in-game review.

### Exact expansion keys

- Institutions `I = {HOS, CHU, MIN, INS}`; families
  `F = {SUP, RES, UTL, PRV}`; ticket numbers
  `N = {01, 02, 03, 04, 05, 06}`. These sets are inclusive, with two-digit
  suffixes.
- For each I, `ENC-I-01`, `-02`, `-03`, `-04`, `-05` mean SUP, RES,
  UTL, PRV, boss respectively. Their vestibules are `ANC-I-01` through
  `ANC-I-05`; closure and transit use `ANC-I-OUT`.
- For each `(I,F,N)`, bind `D-I-F-N`, its `CON-D-I-F-N` and every
  applicable `OBJ-D-I-F-N-{role}`. This expands to 96 distinct tickets,
  not 96 new asset-implied deeds. The four closure IDs are `D-HOS-END`,
  `D-CHU-END`, `D-MIN-END`, `D-INS-END`, with their corresponding
  `CON-D-I-END`.
- For each `(I,F)`, bind `EM-I-F` and its WARN, ACTIVE and AFTER art.
  These are 16 emergencies and 48 state packages, not 48 emergencies.
- Prologue is `ENC-PRO-01`, `ENC-PRO-02`, `ENC-PRO-03` and matching
  `ANC-PRO-01`, `ANC-PRO-02`, `ANC-PRO-03`. Story stages are
  `NAR-01` through `NAR-12` inclusive.
- Ending stages are `END-REVIEW`, `END-CONSENT`, `END-DEATH`, `END-HELL`,
  `END-CEREMONY`, `END-TABLEAU`; recovery also binds `ANC-MIN-PERMIT`,
  `ANC-END-DEATH`, `ANC-END-HELL`, `ANC-END-CEREMONY` and
  `ANC-END-TABLEAU`. Review and consent need ministry UI and staging even
  though the four ending art packages below begin with DEATH.

The actual ticket names, object roles, consequences, encounters and routes
are authoritative in PDD §§8.1–8.9. Expand every role actually used by each
ticket; the family examples below are minimum coverage, not an exhaustive list
of possible object roles.

## Visual and animation inventory

Common visual deliverables: editable layered art or animation project, model
and palette sheets where applicable, RGBA PNG exports and manifest metadata.
Supply previews with player scale and a collision overlay; the overlay is
review-only and must not ship as character art. OWN-ART produces/reviews
against the PDD; OWN-GAME checks event and geometry bindings; OWN-QA records
candidate evidence. Every row also needs the provenance evidence in
`rights-ledger.md`.

| Group and quantity | Campaign bindings and production scope | Specific evidence and review |
|---|---|---|
| `ART-CHR-GRI`, `ART-CHR-VER`, `ART-CHR-POT`, `ART-CHR-SAV`, `ART-CHR-BLA`, `ART-CHR-LEV`, `ART-CHR-NIN`, `ART-CHR-SAT`: **8 sheets**, plus **1 GRI minister costume** | Eight principal characters and portraits across NAR-01–12; GRI variant in NAR-12. POT, SAV, BLA and LEV have visible post-fight damage and poorly concealed ceremony damage. | Each sheet: front, back, both sides, six expressions, swatches and black silhouette; combat sheets add telegraph and exposed-recovery poses. Separate asymmetric text details. Check original expression, readable scale, GRI's four sockets, and continuity into the final tableau. |
| `ART-EN-I-{MEL,RNG}`: **8 appearance sets** | HOS billing orderly/tray and organ courier/jars; CHU collector/pole and penitent/incense; MIN inspector/stamp club and statistics officer/forms; INS follower/selfie stick and coach/binders. INS melee also supports EN-TRE replacement labels. Bind each local service encounter and applicable boss followers. | Distinct weapon silhouettes and preparation poses, dangerous side and ranged release origin; review WARN, attack and harmless defeat against actual events. |
| `ART-RES-I-N`: **24 named appearances and portraits** | Exactly one per RES ticket and named adult in the table below; bind its prompt, refuge sign, encounter, attempt and recovery. | Distinct head, hair, clothing, posture and portrait combinations; protected incapacitation and reset of the same person, not anonymous substitution. |
| `ART-CROWD-I`: **4 kits** | Adult queues in each institution, funeral and local scenes as applicable; MIN includes expressive decomposing workers with name badges and tools. | At least six heads, four torsos and three idle activities per kit. Crowds do not appear to be combat targets. Children are noninteractive evacuation staging only, with no hurt, attack or comic-injury animation. |
| `ART-SET-HOS`, `ART-SET-CHU`, `ART-SET-MIN`, `ART-SET-INS`, `ART-SET-HELL`, `ART-SET-CER`: **6 visual sets** | Four institutions plus NAR-11 Hell and NAR-12 ceremony. Bind entrances, service areas, backgrounds and scene layers. | Palette and material sheets per PDD §9.2.4; HUD-free captures identify each institution. Municipal emblem is an empty chair in an incomplete circle with four queue bars, not a real seal. |
| `ART-BAY-I-F`: **16 dressed bay packages** | Bind each `(I,F)` to the corresponding ENC-I-01–04, all six local tickets, vestibule, working bay, certification area and bypass. | Distinct wall stories, consignments and aftermath; consistent SUP arrows/seals, RES plate/latch/refuge, UTL circuit/paddle, PRV recorder/lens symbols. Validate entrances, safe refuge, terminal and bypass readability. |
| `ART-BOSS-I`: **4 arena, machinery and surviving-defeat packages** | ENC-I-05, ANC-I-05 and ANC-I-OUT; Potroshin, Savva, Blagoev, Viralsky. | Each entire 1,280-pixel arena, protected refill alcoves, safe routes, phase machinery and living antagonist after defeat. Provide a three-phase timing preview linked to every local attack in §8.7. MIN gantry remains visibly dry; INS relays A and B show their active protection. |
| `ART-PRO-01`, `ART-PRO-02`, `ART-PRO-03`: **3 packages** | Charity annex and deliberate theft, pursuit/heart attack, funeral/counter and ledger bargain; ENC-PRO-01–03, ANC-PRO-01–03, NAR-01–03. | Scene layers and contact sheets preserve readable donation label, Nina's reaction, funeral reversal and the visible 100-deed explanation. No gameplay-death fee is implied. |
| `ART-TRANSIT-HOS`, `ART-TRANSIT-CHU`, `ART-TRANSIT-MIN`, `ART-TRANSIT-INS`: **4 packages** | Safe institution OUT stops, destination signs and unlocked connections, including INS return to the ministry permit lobby. | Signs and route markers match the actual unlocked destination and do not imply another location or credit. |
| `ART-OBJ-{family}-{role}`: **shared role sets**, with **4 institutional finishes and 6 ticket-label bindings per local family** | Expand all 96 ticket uses and required OBJ roles. Include sealed trolley/receiving pad and terminal; plate, crate, gate, latch and refuge; paddle, bypass, fuse and meter; recorder, camera receptor and public terminal. Include refill, checkpoint, source, relay and closure equipment where used. | Show intact/broken seals; RES closed, weighted-open and latched-open gates; UTL unlatched/latched paddles; PRV clear, fouled and recorded-interruption states. Ticket labels and variant hazard plans must not cross-bind. |
| `ART-EM-I-F-{WARN,ACTIVE,AFTER}`: **48 packages** | Each of the 16 EM-I-F IDs gets all three states, tied to the sixth *certification*, whichever ticket it is. Bind individual CON aftermath separately. | Show the §8.8.1 local event, refuge and persistent aftermath; test the 1.5-second warning, active hazard and safe bypass against actual state. A detailed expansion follows. |
| `FX-BODY`, `FX-FEC`, `FX-IMPACT`, `FX-HAZARD`: **4 reusable families**, each with reduced-particle variant | All relevant impact, limb, projectile, smear, hazard, emergency and boss events. | Include readable active projectile, settled limb, harmless debris, player feces and harmful ministry waste distinctions. Keep essential overlays above effects; normal hit spray ≤400 ms, removal spray ≤600 ms, ≤48 persistent decorative stains per room. |
| `ART-END-DEATH`, `ART-END-HELL`, `ART-END-CEREMONY`, `ART-END-TABLEAU`: **4 complete packages** | END-DEATH, END-HELL, END-CEREMONY and END-TABLEAU; NAR-10–12 and their ending anchors. | Editable foreground, character, effect and separate text layers. Include consent/permit staging via the ministry assets and UI. Review full, skipped, interrupted and replayed ending. The tableau requirements below are mandatory. |

Named RES portrait and animation bindings:

| Institution | `01` | `02` | `03` | `04` | `05` | `06` |
|---|---|---|---|---|---|---|
| HOS | Мария, санитарка | Пётр, пациент | Алла, техник | Борис, пациент | Зоя, медсестра | Семён, уборщик |
| CHU | Тамара, волонтёр | Илья, постоялец | Людмила, швея | Павел, гробовщик | Оксана, прихожанка | Денис, архивист |
| MIN | Галина, заявительница | Виктор, архивист | Роза, уборщица | Михаил, заявитель | Эмма, статистик | Анатолий, курьер |
| INS | Лидия, слушательница | Артём, монтажёр | Светлана, заявительница | Олег, рабочий сцены | Елена, подписчица | Фёдор, связист |

Emergency package expansion; each cell names one EM identity and its
WARN/ACTIVE/AFTER visual progression. Retain the actual refuge and durable
consequence in PDD §8.8.1:

| I | SUP | RES | UTL | PRV |
|---|---|---|---|---|
| HOS | `EM-HOS-SUP`: overloaded junction / steam / barrier | `EM-HOS-RES`: queue gate / bed wave / wrecked beds | `EM-HOS-UTL`: pressure gauge / manifold burst / manual bypass | `EM-HOS-PRV`: sparking rack / fire / burnt screens |
| CHU | `EM-CHU-SUP`: strained conveyor / burning candles / wax and tins | `EM-CHU-RES`: failing turnstile / queue spill / broken turnstile | `EM-CHU-UTL`: overload / falling bell / exhibit | `EM-CHU-PRV`: server fault / projected confessions / paid scandal |
| MIN | `EM-MIN-SUP`: jammed press / paperwork wave / processing report | `EM-MIN-RES`: work counter / desk wave / separate desks | `EM-MIN-UTL`: waste gauge / discharge / contaminated-floor barrier | `EM-MIN-PRV`: archive fault / waste samples / audit report |
| INS | `EM-INS-SUP`: overloaded chute / boxes / sales graphic | `EM-INS-RES`: strained arch / collapse / separate exit | `EM-INS-UTL`: studio meter / shorted grid / replacement ads | `EM-INS-PRV`: tracking rack / trend projections and marked arcs / attention report |

### Animation register

Supply editable rigs or drawing sequences, atlases, preview playback and
per-clip metadata. OWN-ART reviews the animation and OWN-GAME checks
authoritative event traces; OWN-QA captures resulting play. Timing in PDD
§§7, 8.7 and 9.4 is authoritative: visual interpolation must not change a
simulation window or conceal an interruption.

- **18 GRI clips:** `AN-GRI-IDLE`, `RUN`, `HOP`, `CRAWL`, `JUMP`,
  `EVADE`, `MELEE`, `THROW`, `REMOVE`, `TEETH`, `REPLACE`, `REGEN`,
  `HURT`, `SLIP`, `PROP`, `INTERACT`, `DEAD`, `RESTORE`, each with the
  full `AN-GRI-` prefix. Use separate torso, head, socket and limb layers.
  Cover all 16 combinations of `LIM-LA`, `LIM-RA`, `LIM-LL`, `LIM-RL`;
  author both-arm and both-leg loss transitions rather than stretching a run
  into a crawl. Include no-arm headbutt, teeth-assisted removal, no-leg torso
  movement and interaction/refill variants.
- Preserve the PDD pose and timing briefs: idle six poses/1 s, run eight/
  500 ms, hop six/600 ms, crawl six/700 ms, jump five poses, evade three/
  180 ms with its first 120 ms marked; melee 120/80/250 ms, throw
  100/50/250 ms, removal 350 ms, replacement 400 ms, regeneration 1.5 s,
  hurt 180 ms, slip 350 ms and prop reaction within 700 ms. Simulation's
  upward tick quantization determines the actual event boundaries.
- **64 enemy state uses:** for each `I`, each `{MEL,RNG}`, and each
  `{IDLE, APPROACH, TELEGRAPH, ACTIVE, RECOVERY, HURT, DISTRACTION, DEFEAT}`,
  expand `AN-EN-I-{MEL or RNG}-{state}`. The INS melee set also serves
  EN-TRE, with its visible trend and source indicators.
- **120 RES state uses:** for each `I`, `N` and
  `{WAIT, FOLLOW, SAFE-STOP, REFUGE, INCAPACITATED}`, expand
  `AN-RES-I-N-{state}` and bind the named adult above.
- **36 boss attack-phase uses:** for each attack
  `{HOS-A,HOS-B,HOS-C, CHU-A,CHU-B,CHU-C, MIN-A,MIN-B,MIN-C,
  INS-A,INS-B,INS-C}` and phase `{PREP, ACTIVE, RECOVER}`, expand the
  PDD's `AN-BOSS-{institution}-{attack ID}-{phase}` form. Add transition
  and surviving-defeat clips for each of the four bosses. Hold authored
  preparation drawings as necessary; never shorten a warning to fit a clip.

Event metadata must mark actual release, impact, footfall, separation, socket
change, channel completion, damage, latch, interruption, protection and
exposure where applicable. A canceled action must not show a completed resource
change. The patterned hurt-immunity outline and crossed-slip icon are
distinct and do not rely on flashing.

### Interface, signs and fonts

OWN-ART supplies editable panels, symbols, focus and marker variants and PNG
exports; OWN-RU supplies separately addressable text; OWN-PLAT integrates
font files and binding-aware glyphs; OWN-QA captures minimum-size and input
coverage. Verify all three text sizes and accessibility combinations.

- **Nine HUD assets:** `UI-HUD-BODY`, `UI-HUD-PAYLOAD`, `UI-HUD-CREDIT`,
  `UI-HUD-OBJECTIVE`, `UI-HUD-BOSS`, `UI-HUD-CHANNEL`, `UI-HUD-NOTICE`,
  `UI-HUD-CAPTION`, `UI-HUD-SAVE`. Cover selected and disabled states,
  exact resources, separate debt and committed credits, named boss protection
  and exposure, interruption, receipt and consequence, source/direction
  captions, and pending/saved/error distinctions.
- **Ten save/recovery feedback identities:** `UI-SAV-PENDING`, `UI-SAV-OK`,
  `UI-SAV-ERR`, `UI-REC-DEAD`, `UI-REC-RETURN`, `UI-REC-LOAD`,
  `UI-REC-READY`, `UI-REC-OLDER`, `UI-REC-BROKEN`, `UI-REC-SETTINGS`.
  Include explicit choices, preserved totals, reset scope, named anchor and
  Cancel-first destructive confirmations as applicable.
- `UI-MENU`, `UI-LEDGER`, `UI-REC` and supporting symbol families cover
  adult-content notice, title, Continue, pause, focus/device loss, display,
  audio, interface, accessibility, controls, tutorials, filters, all 100
  receipt rows, consequences, failures, settings and ending replay. Cover
  normal, focus, selected, disabled and confirmation states; keyboard, mouse
  and controller prompts resolve current bindings, not painted key labels.
- Supply danger serrations/inward triangles, refuge doorway/double line,
  interaction brackets, payload receptor ring, trajectory and distinct
  terrain/valid/invalid first-impact markers, boss closed-hatch and broken
  brackets, relay labels, and high-contrast variants. Meaning must survive
  muted greyscale play.
- **Noto Sans Regular, SemiBold and Bold**: three Cyrillic-capable font
  files or approved equivalent files of the specified family, with actual
  license text and version/source provenance. Keep fonts separate from
  atlases. Test all shipped strings, including А–Я, а–я, Ё, ё, Latin,
  digits, punctuation, «», №, ₽ and $ at 100%, 125%, 150%. Original
  title/emblem lettering is separate and never replaces essential text.
- Essential signage uses a blank art panel and separately editable Russian
  text. Keep a 24-logical-pixel safe margin; normal UI 24 px, supporting
  labels at least 20 px, dialogue 26 px, headers at least 32 px. Text
  contrast targets ≥4.5:1 and essential marker edges ≥3:1.

### Final tableau

`ART-END-TABLEAU` is one **uncropped 16:9 composition**, visible together at
1,280 × 720 and after a scene skip. Grisha and the casino ribbon occupy the
lower center; Potroshin, Savva, Blagoev and Viralsky flank him. The children's
wing occupies at least the upper-right quarter, with visible flames and the
sign `ДЕТСКОЕ ОТДЕЛЕНИЕ`; smoke must not hide it. Nina leads evacuation on
the opposite side. A clear upper band carries the exact closing text
`«ПОЗДРАВЛЯЕМ! ВЫ СДЕЛАЛИ МИР ЛУЧШЕ».` without covering the wing, ribbon or
evacuation. The player must acknowledge it before completion. Children are
not shown as injured or given gameplay reactions.

## Audio inventory

OWN-AUD supplies editable sessions, scores, clean masters, stems or tails
needed for transitions, runtime exports, loop and transition metadata, caption
bindings and mix references. OWN-RIGHTS reviews composition, recording,
sample and performer evidence; OWN-PLAT integrates semantic events; OWN-QA
captures candidate playback and measurements.

### Music: 18 cues

The four institutional rows each expand into three separate cue records,
not one combined track. For each institution, EXP is a 96-second loop, CBT
a 64-second loop and BOS a 96-second loop.

| IDs and count | Binding and production direction |
|---|---|
| `MUS-HOS-EXP`, `MUS-HOS-CBT`, `MUS-HOS-BOS` — 3 | Hospital exploration, combat and boss; cornet, strings, trolley percussion, detuned piano. |
| `MUS-CHU-EXP`, `MUS-CHU-CBT`, `MUS-CHU-BOS` — 3 | Church; reed organ, cash-drawer percussion, dry choir vowels, bowed strings. |
| `MUS-MIN-EXP`, `MUS-MIN-CBT`, `MUS-MIN-BOS` — 3 | Ministry; bass clarinet, stamp percussion, snare, brass. |
| `MUS-INS-EXP`, `MUS-INS-CBT`, `MUS-INS-BOS` — 3 | Institute; original synthetic sales fragments, bass, binder slaps, keyboard; no real advertisement melody. |
| `MUS-TITLE` — 1 | Title; 64-second loop. |
| `MUS-PRO` — 1 | Charity and pursuit; 48-second cue with heart-attack break marker. |
| `MUS-FUNERAL` — 1 | Funeral; 48-second loop with resurrection stop. |
| `MUS-HELL` — 1 | Hell; 64-second loop with space for rejection. |
| `MUS-CEREMONY` — 1 | Casino ceremony; 48-second cue with final held chord. |
| `MUS-CREDITS` — 1 | Credits; 120-second loop. |

The original administrative motif has two rising notes and an unresolved
descending answer; its exact pitches belong in the delivered score. Provide
clean starts, loop points, transition markers and short ending tails. Combat
crossfades over one second; phase accents do not restart the boss cue. No
licensed song, sampled film line, reference-soundtrack imitation or copied
advertising melody is authorized by the PDD.

### Stingers, ambience, effects and voices

| Group and quantity | Required uses, sources and acceptance evidence |
|---|---|
| **8 stingers:** `STG-CREDIT`, `STG-CONSEQUENCE`, `STG-FAIL`, `STG-CHECKPOINT`, `STG-DEATH`, `STG-VICTORY`, `STG-PERMIT`, `STG-ENDING` | Separate editable sessions and short runtime effects. Credit is <1 s; consequence uses a broken mechanical descent. Victory and receipt effects follow durable success; receipt never masks an emergency warning. |
| **6 ambience beds:** `AMB-HOS`, `AMB-CHU`, `AMB-MIN`, `AMB-INS`, `AMB-HELL`, `AMB-CEREMONY` | Institution pumps/queues/mechanisms, Hell pages and furnace, ceremony applause/electrical strain/fire/evacuation respectively. Supply clean loops and sample boundaries; essential instructions also appear as text. |
| `SFX-MOVE` | Four footfall variations **each** for tile, wood, concrete and metal, plus jump and landing sets; bind movement and actual footfall events. |
| `SFX-BODY` | Swing, flesh impact, hurt, tear, replacement, regeneration, headbutt, collapse; at least three variations per repeated body-impact event. Tear combines fabric, wet pull and vocal effort, not a real injury recording. |
| `SFX-LIMB` | Arm flight, leg flight, damaging impact, harmless bounce, retrieval and plate contact; distinguish live flight from settled objects. |
| `SFX-FEC` | Release, direct hit, receptor hit, floor smear, slip, empty resource and refill. Distinguish player feces from harmful ministry waste, including pipe pressure and sustained waste hazard. |
| `SFX-SERVICE` | Seal, trolley, plate, latch, paddle, bypass, meter, recorder and camera interruption across all four families and state changes. |
| `SFX-WARN` | Enemy preparation, ranged release, hazard, crusher and emergency warnings; each begins with its actual telegraph and remains distinguishable. |
| `SFX-BOSS-I` — **4 families, 12 attacks × 3 phases = 36 phase uses** | Separate PREP, ACTIVE and RECOVER for HOS-A/B/C, CHU-A/B/C, MIN-A/B/C and INS-A/B/C. Preparation sounds distinguish attacks; bind the same event/phase used by animation. |
| `SFX-SOURCE` | Recruitment queue, spawn warning, source hit and stop, relay protection and break; all institute feeds and boss relays. |
| `SFX-UI` | Focus, confirm, cancel, invalid input, ledger page, pending save, saved result and storage error; preserve semantic distinctions. |
| `SFX-END` | Final-death equipment, Hell records, appointment stamp, power diversion, ribbon cut, applause and fire; bind END-DEATH through TABLEAU. |
| `VOC-{character ID}-{EFFORT,HURT,LAUGH,REACT}` | Principal characters GRI, VER, POT, SAV, BLA, LEV, NIN, SAT where each action requires a category. Record at least three clean takes for **each used category**. Document unused categories as not applicable with review, not as silent delivered files. |
| `VOC-TRE-SIXSEVEN`, `VOC-TRE-LEVEL`, `VOC-TRE-BRAND` — **3 lexical sets** | Newly recorded ensemble shouts: “SIX SEVEN”, “НОВЫЙ УРОВЕНЬ”, “ЛИЧНЫЙ БРЕНД”. First replacement changes label; later replacements alternate established labels. One intelligible foreground chant at a time, ≥4 s global interval; visible warning and caption remain when muted. |

Each effect has one semantic event owner; cosmetic bounces cannot replay
attack or certification sounds. Document the clean take, source and any
processing for each exported variation. Text-led story dialogue does not
require full spoken Russian lines or lip-sync, but its captions and short
nonverbal performances remain required.

Reference mixes target -16 LUFS integrated within ±2 LU and true peak ≤-1
dBTP. Review headphones, small laptop speakers at low volume, mono and reduced
dynamic range. Essential warnings survive simultaneous stingers, chants,
impacts and ambience. Master/Music/Effects/Ambience/Vocal defaults are
80/65/90/55/80%, each adjustable 0–100%; warning ducking and dialogue
ducking follow PDD §9.7.4. Retain raw measurement and listening evidence.

## Russian text and captions

OWN-RU authors and edits a complete versioned Russian catalogue; OWN-GAME
binds IDs to state; OWN-PLAT validates glyphs, layout and current control
tokens; OWN-QA captures candidate screens. Editable source is the
ID-addressed text catalogue and scene script; runtime exports are the
versioned UTF-8 content and bindings, not text baked into images. Rights
records cover each contribution and source.

Required groups and counts:

- `TXT`, `DLG`, `TUT`, `CAP`: **four catalogues**, with an individual
  stable record for every actual line, sign, instruction, caption and
  variable form. PDD examples are not the entire script. Cover all NAR-01–12,
  20 institutional encounters, three prologue encounters, transit, four
  closures, six ending stages and all recovery states.
- **100 receipt titles and rows:** 96 `(I,F,N)` tickets and four closures,
  plus their exact evidence, failure, retry, consequence and emergency text.
  Bind the named recipients, local object labels, 16 EM warnings and
  aftermath, 100 CON records and anchors. A receipt is not a new deed.
- **11 tutorial identities:** `TUT-MOV`, `TUT-INT`, `TUT-LED`, `TUT-CBT`,
  `TUT-CRD`, `TUT-LIM`, `TUT-UTL`, `TUT-FEC`, `TUT-REC`, `TUT-BOSS`,
  `TUT-SOURCE`. Include first-use acknowledgment, practice labels and
  ledger archive entries.
- **Seven example evidence identities:** `TXT-EVD-SUP`, `TXT-EVD-RES`,
  `TXT-EVD-UTL`, `TXT-EVD-PRV`, `TXT-EVD-READY`, `TXT-EVD-FAILED`,
  `TXT-EVD-EXISTING`; add all actual safe Russian failure reasons rather
  than inserting internal errors.
- Complete title, 18+ notice, menus, current-binding prompts, settings,
  ledger, save and recovery copy, confirmations, captions for barks and
  essential sounds with speaker/source and direction, signs and narrative
  documents. Include the literal funeral lines, the exact five-line Hell
  exchange and exact closing text from PDD §§6.2, 6.11 and 6.12.
- Use named fields and complete grammatical forms, correct ё, decimal comma,
  established dollar debt and whole-number credits. Keep location names
  consistent with PDD §4. Dialogue normally uses two lines of at most 68
  characters; larger text can use three, with long speeches split into
  player-advanced units. Do not shrink below the minimum to force a fit.

Native Russian editorial review must cover the complete script and rendered
catalogue: grammar, ё, tone, profanity, character voices, victim agency,
variable forms, exact anchors, names, captions, instruction completeness and
clipping. References are tonal influences, not material to copy.

## Shared technical acceptance

- Logical canvas and gameplay view: 1,280 × 720, 16:9; default presentation
  1,920 × 1,080. Letterbox other ratios. Keep the entire boss arena and
  mandatory landing markers visible. Layer essential outlines, trajectory and
  prompts above gore and below HUD.
- Source art is 2× logical resolution: the normal 48 × 80 logical character
  envelope begins at 96 × 160 source pixels. Player pivot is the bottom
  center of the standing collision box; socket positions remain consistent
  across clips. Extra motion envelope does not change collision.
- Sprite and tile exports are sRGB RGBA PNG; atlases ≤2,048 × 2,048, with
  at least four **source pixels** of edge extrusion and padding per sprite,
  no hidden matte fringes. Environments use 32-logical-pixel structural units.
  Divide large backgrounds into compliant layers.
- Audio masters are 48 kHz/24-bit PCM WAV. Short runtime effects are
  48 kHz/16-bit PCM WAV; music and long ambience are 48 kHz Ogg Vorbis,
  approximately 160–192 kbps stereo. Appropriate spot effects remain mono.
  Record sample-accurate loop boundaries, tails, channel layout and loudness;
  inspect for clicks and clipping.
- Accessibility variants include 100/125/150% text, captions and opaque
  caption backing, Off/Low/Full shake, reduced motion, high-contrast
  markers, reduced blood particles, hold assistance, reduced dynamic range
  and mono. Preserve wounds, missing limbs, adult content, consequence and
  authoritative timings. No essential rapid flash; decorative flashes ≤2/s.
  Do not claim blind-play or full screen-reader support.
- Each principal encounter has a contact sheet showing player scale, terrain,
  telegraphs, HUD, effects and aftermath. Each boss has a three-phase timing
  preview linked to §8.7. Record visual collision-overlay previews separately
  from shipped art.

## Handoff and review sequence

1. OWN-PROD records roles, separately authorized provision and delivery
   scope. No provider, contract, schedule or budget is assumed here.
2. OWN-ART, OWN-AUD and OWN-RU deliver editable source, exports, expanded
   asset/use manifest and technical metadata above. They identify every
   derivative, sample, reference, contributor and performer to OWN-RIGHTS.
3. OWN-RIGHTS checks the ledger, actual evidence, license conditions,
   notices and comparative originality review. Unresolved evidence blocks
   acceptance; review records concerns and is not legal clearance.
4. OWN-GAME and OWN-PLAT integrate delivered files through content and
   native boundaries, validate references and generate the expanded
   manifest and content digest with checked commands. Missing required files
   fail content validation; no production fixture selector or invented hash.
5. OWN-RU reviews the full Russian catalogue and rendered candidate.
   OWN-ART and OWN-AUD review final visuals, event timing and mixes. Return
   defects to the responsible producer; replacements require renewed
   provenance, integration and affected reviews.
6. OWN-QA executes the parameterized PRE and FGA cases on the identified
   packaged candidate. OWN-PROD reviews phase evidence; readiness does not
   authorize publication.

## PRE traceability and required observations

All rows are **not run**; final-media or target prerequisites are currently
**blocked**. Refer to PDD §9.11 for the full setup, not just this index.

| Scenario | Inventory / evidence to inspect | Integrated reference and reviewer |
|---|---|---|
| PRE-T01 | Sheets, palettes, scenes, music and rights; comparative original-expression review. | FGA-V01/N03; OWN-ART and OWN-RIGHTS, QA records. |
| PRE-T02 | Capture each of four entrances, working bays and boss arenas without HUD. | FGA-N03/V01; OWN-ART, QA. |
| PRE-T03 | GRI movement and all 16 limb combinations at every text size. | FGA-V02/G04/G05; OWN-ART, QA. |
| PRE-T04 | Damage-interrupted removal, throw, refill and regeneration; animation, sound, channel and resource agree. | FGA-V02/G04–G06; OWN-ART, QA. |
| PRE-T05 | All four bosses and their attacks, muted and greyscale; routes, protection and relays distinct. | FGA-V03; OWN-ART, QA. |
| PRE-T06 | All 16 sixth-certification emergencies under full permitted effects; separate credit, consequence, warning, refuge and bypass. | FGA-V03; OWN-ART, QA. |
| PRE-T07 | Active projectiles, settled limbs, debris, smears and harmful waste together, without color/sound dependence. | FGA-V03; OWN-ART, QA. |
| PRE-T08 | Exact receipt and repeat behavior, 99 and 100 objectives; no false stamp. | FGA-C03/V04; QA and domain owners. |
| PRE-T09 | Simultaneous credit/death and interrupted storage; pending, saved, rollback, fee and recovery agree. | FGA-R01–R09/V04; QA and domain owners. |
| PRE-T10 | Focus/controller loss, checkpoint return, older and both-invalid recovery; cause, totals and choices. | FGA-R01–R09/V04; QA and domain owners. |
| PRE-T11 | Remapped keyboard, mouse and controller prompts and focus, including tutorials; no hover-only action. | FGA-G10/V05/B04; OWN-RU and OWN-PLAT, QA. |
| PRE-T12 | Entire Russian catalogue at 1,280 × 720 and all three sizes, including glyphs and variables. | FGA-V05/B04; native Russian editor and OWN-PLAT, QA. |
| PRE-T13 | Encounter listening on laptop speakers, mono and reduced dynamic range; warnings and chants. | FGA-V06/B06; OWN-AUD, QA. |
| PRE-T14 | Viewed, skipped, interrupted and replayed ending, including mandatory tableau. | FGA-N04–N06/R09; OWN-RU and OWN-ART, QA. |
| PRE-T15 | Uncropped END-TABLEAU at minimum size with every required element and exact text together. | FGA-N06; OWN-RU and OWN-ART, QA. |
| PRE-T16 | Combined reduced particles/motion, no shake, captions and high contrast; required meaning and adult premise remain. | FGA-V07; OWN-ART and OWN-AUD, QA. |
| PRE-T17 | Expanded manifest against every ENC, OBJ, DeedID, CON, EM, ANC, NAR and END use; no additional credit. | FGA-V08; OWN-ART, OWN-AUD and OWN-RIGHTS. |
| PRE-T18 | Source/export handoff: dimensions, pivots, sockets, markers, loops, licenses and captions. | FGA-V08; OWN-ART, OWN-AUD and OWN-RIGHTS. |
