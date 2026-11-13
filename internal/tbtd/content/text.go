// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"
)

// TextEntry is one independently addressable text layer or player-advanced
// dialogue unit. Reference is a campaign location, encounter, story or ending
// identity; it is not a media filename. Speaker is a stable actor identity.
type TextEntry struct {
	ID string `json:"id"`

	Text string `json:"text"`

	Reference string `json:"ref,omitempty"`

	Speaker string `json:"speaker,omitempty"`
}

type textDocument struct {
	SchemaVersion int `json:"schema_version"`

	Entries []TextEntry `json:"entries"`
}

// RussianText is an immutable, strictly validated Russian catalogue. Its
// campaign service titles come from the same validated manifest as receipts.
type RussianText struct {
	entries map[string]TextEntry
	ordered []TextEntry
}

var textFiles = []struct {
	path   string
	prefix string
}{
	{"content/data/ru-interface.json", "TXT"},
	{"content/data/ru-dialogue.json", "DLG"},
	{"content/data/ru-tutorials.json", "TUT"},
	{"content/data/ru-captions.json", "CAP"},
	{"content/data/ru-signs.json", "TXT"},
}

// templateFields is the caller contract, independent of the authored wording.
// Every other entry must contain no substitutions.
var templateFields = map[string]string{
	"TXT-MENU-NEW-WARN": "count",

	"TXT-MENU-SKIP": "scene",

	"TXT-MENU-LOAD": "institution anchor count",

	"TXT-MENU-FINALE": "stage",

	"TXT-HUD-HEALTH": "count",

	"TXT-HUD-FECES": "count",

	"TXT-HUD-CREDIT": "count",

	"TXT-HUD-DEBT": "amount",

	"TXT-HUD-PROTECTED": "reason",

	"TXT-HUD-PHASE": "count",

	"TXT-HUD-BLOCKED": "reason",

	"TXT-HUD-OBJECTIVE": "service action",

	"TXT-LED-RECEIPT": "service institution evidence count",

	"TXT-LED-BATCH": "supply rescue utility privacy closure",

	"TXT-EVD-FAILED": "reason",

	"TXT-RECEIPT-DETAIL": "service count",

	"TXT-CONSEQUENCE-DETAIL": "service consequence",

	"TXT-EMERGENCY-ESCAPE": "danger",

	"TXT-GATE-MISSING": "reason",

	"TXT-SAVE-DETAIL": "count event",

	"TXT-SAVE-DISCARD": "event",

	"TXT-DEATH-DESTINATION": "anchor count amount",

	"TXT-REC-LOAD": "institution anchor count",

	"TXT-REC-OLDER": "anchor count difference",

	"TXT-CHECKPOINT": "anchor",

	"CAP-DIRECTION": "direction sound",

	"TUT-MOV": "move jump down",

	"TUT-INT": "interact",

	"TUT-LED": "ledger",

	"TUT-CBT": "melee evade",

	"TUT-CRD": "interact",

	"TUT-LIM": "select remove interact regenerate",

	"TUT-LIM-PLATE": "interact",

	"TUT-UTL": "aim throw interact",

	"TUT-FEC": "select aim throw interact",
}

var requiredText = []string{
	"TXT-TITLE-SUBTITLE TXT-NOTICE-ADULT TXT-MENU-CONTINUE TXT-MENU-NEW TXT-MENU-SETTINGS TXT-MENU-EXIT TXT-MENU-RESUME TXT-MENU-LEDGER TXT-MENU-CONTROLS TXT-MENU-RETURN TXT-MENU-TITLE TXT-MENU-CONFIRM TXT-MENU-CANCEL TXT-MENU-RETRY TXT-MENU-REPLAY TXT-MENU-END TXT-MENU-NEW-WARN TXT-MENU-QUIT-WARN TXT-MENU-SWITCH-WARN TXT-MENU-SKIP TXT-MENU-FOCUS TXT-MENU-CONTROLLER TXT-MENU-LOAD TXT-MENU-FINALE",
	"TXT-SET-DISPLAY TXT-SET-AUDIO TXT-SET-INTERFACE TXT-SET-ACCESS TXT-SET-CONTROLS TXT-SET-WINDOWED TXT-SET-BORDERLESS TXT-SET-FULLSCREEN TXT-SET-RESOLUTION TXT-SET-VSYNC TXT-SET-APPLY TXT-SET-DISPLAY-CONFIRM TXT-SET-DISPLAY-FAIL TXT-SET-MASTER TXT-SET-MUSIC TXT-SET-EFFECTS TXT-SET-AMBIENCE TXT-SET-VOICE TXT-SET-TEXT-SIZE TXT-SET-DIALOGUE-CAPTIONS TXT-SET-SOUND-CAPTIONS TXT-SET-CAPTION-BACK TXT-SET-SHAKE TXT-SET-MOTION TXT-SET-CONTRAST TXT-SET-BLOOD TXT-SET-HOLD TXT-SET-HOLD-DESC TXT-SET-RANGE TXT-SET-MONO TXT-SET-OFF TXT-SET-ON TXT-SET-LOW TXT-SET-FULL TXT-SET-REDUCED TXT-SET-DEFAULTS TXT-SET-DUPLICATE TXT-SET-CONTENT TXT-SET-SAVE-FAIL",
	"TXT-CTRL-LEFT TXT-CTRL-RIGHT TXT-CTRL-DOWN TXT-CTRL-JUMP TXT-CTRL-AIM TXT-CTRL-MELEE TXT-CTRL-THROW TXT-CTRL-EVADE TXT-CTRL-INTERACT TXT-CTRL-PAYLOAD TXT-CTRL-LARM TXT-CTRL-RARM TXT-CTRL-LLEG TXT-CTRL-RLEG TXT-CTRL-REMOVE TXT-CTRL-REGEN TXT-CTRL-LEDGER TXT-CTRL-PAUSE TXT-CTRL-MENU TXT-CTRL-RECOVERY",
	"TXT-HUD-HEALTH TXT-HUD-FECES TXT-HUD-CREDIT TXT-HUD-DEBT TXT-HUD-NO-LEGS TXT-HUD-NO-ARMS TXT-HUD-LOW TXT-HUD-PROTECTED TXT-HUD-EXPOSED TXT-HUD-PHASE TXT-HUD-CHANNEL-COST TXT-HUD-INTERRUPTED TXT-HUD-EMPTY TXT-HUD-BLOCKED TXT-HUD-OBJECTIVE",
	"TXT-LED-SERVICES TXT-LED-CONSEQUENCES TXT-LED-TUTORIALS TXT-LED-AVAILABLE TXT-LED-ACTIVE TXT-LED-READY TXT-LED-CERTIFIED TXT-LED-FAILED TXT-LED-WARNING TXT-LED-EMERGENCY TXT-LED-AFTERMATH TXT-LED-TRACK TXT-LED-RESET TXT-LED-RECOVERY TXT-LED-RECEIPT TXT-LED-BATCH",
	"TXT-EVD-SUP TXT-EVD-RES TXT-EVD-UTL TXT-EVD-PRV TXT-EVD-READY TXT-EVD-FAILED TXT-EVD-EXISTING TXT-FAIL-SEAL TXT-FAIL-ADULT TXT-FAIL-FUSE TXT-FAIL-RECORD TXT-FAIL-TARGET TXT-RECEIPT-HEADING TXT-RECEIPT-DETAIL TXT-CONSEQUENCE-HEADING TXT-CONSEQUENCE-DETAIL TXT-ATTEMPT-HEADING TXT-EMERGENCY TXT-EMERGENCY-ESCAPE TXT-OBJECTIVE-99 TXT-OBJECTIVE-100 TXT-GATE-MISSING TXT-PRO-THEFT TXT-PRO-THEFT-CONFIRM TXT-CONSENT TXT-CONSENT-WARN",
	"TXT-SAVE-PENDING TXT-SAVE-OK TXT-SAVE-ERROR TXT-SAVE-DETAIL TXT-SAVE-RETRY TXT-SAVE-RELOAD TXT-SAVE-EXIT TXT-SAVE-DISCARD TXT-DEATH-HEADING TXT-DEATH-PRESERVED TXT-DEATH-DESTINATION TXT-RETURN-WARN TXT-RETURN-NOFEE TXT-REC-LOAD TXT-REC-RESET TXT-REC-READY TXT-REC-OLDER TXT-REC-UNKNOWN TXT-REC-BROKEN TXT-REC-SELECTOR TXT-REC-SELECT TXT-REC-NEW-CONFIRM TXT-REC-SETTINGS TXT-CHECKPOINT TXT-REPLAY-INFO TXT-ENDING-ACK TXT-ENDING-TABLEAU",
	"TUT-MOV TUT-INT TUT-LED TUT-CBT TUT-CRD TUT-LIM TUT-LIM-PLATE TUT-UTL TUT-FEC TUT-FEC-PRACTICE TUT-REC TUT-REC-DEATH TUT-BOSS TUT-SOURCE",
	"TXT-SIGN-HOS TXT-SIGN-CHU TXT-SIGN-MIN TXT-SIGN-INS TXT-SIGN-HOS-DONATION TXT-SIGN-HOS-LIFE TXT-SIGN-HOS-WING TXT-SIGN-HOS-DISPATCH TXT-SIGN-HOS-VOLUNTEER TXT-SIGN-CHU-CONFESSION TXT-SIGN-CHU-ARCHIVE TXT-SIGN-MIN-CONTROL TXT-SIGN-MIN-PERMIT TXT-SIGN-INS-SOURCE TXT-SIGN-INS-EXIT TXT-SIGN-APPOINTMENT TXT-SIGN-CASINO",
	"CAP-DIRECTION CAP-LEFT CAP-RIGHT CAP-ABOVE CAP-NEAR CAP-MOVE CAP-JUMP CAP-LAND CAP-SWING CAP-HIT CAP-HURT CAP-TEAR CAP-REPLACE CAP-REGEN CAP-HEADBUTT CAP-COLLAPSE CAP-LIMB-FLIGHT CAP-LIMB-IMPACT CAP-LIMB-BOUNCE CAP-LIMB-RETRIEVE CAP-PLATE CAP-FEC-RELEASE CAP-FEC-HIT CAP-FEC-CAMERA CAP-FEC-SMEAR CAP-FEC-SLIP CAP-FEC-EMPTY CAP-FEC-REFILL CAP-SERVICE-SEAL CAP-SERVICE-TROLLEY CAP-SERVICE-LATCH CAP-SERVICE-PADDLE CAP-SERVICE-BYPASS CAP-SERVICE-METER CAP-SERVICE-RECORDER CAP-SERVICE-CAMERA CAP-WARN-ENEMY CAP-WARN-RANGED CAP-WARN-HAZARD CAP-WARN-CRUSHER CAP-WARN-EMERGENCY CAP-SOURCE-QUEUE CAP-SOURCE-SPAWN CAP-SOURCE-HIT CAP-SOURCE-STOP CAP-RELAY-PROTECT CAP-RELAY-BREAK CAP-TREND-SIXSEVEN CAP-TREND-LEVEL CAP-TREND-BRAND CAP-UI-FOCUS CAP-UI-CONFIRM CAP-UI-CANCEL CAP-UI-INVALID CAP-UI-LEDGER CAP-UI-PENDING CAP-UI-SAVED CAP-UI-ERROR CAP-END-DEATH CAP-END-HELL CAP-END-APPOINTMENT CAP-END-POWER CAP-END-RIBBON CAP-END-APPLAUSE CAP-END-FIRE",
	"TXT-CHR-GRI TXT-CHR-VER TXT-CHR-NIN TXT-CHR-POT TXT-CHR-SAV TXT-CHR-BLA TXT-CHR-LEV TXT-CHR-SAT TXT-CHR-CROWD TXT-CHR-FOLLOWERS",
	"TXT-RES-FOLLOW TXT-ACT-SUP-PAD TXT-ACT-SUP-CERTIFY TXT-ACT-RES-GATE TXT-ACT-RES-WAIT TXT-ACT-RES-REFUGE TXT-ACT-UTL-PADDLE TXT-ACT-UTL-BYPASS TXT-ACT-UTL-METER TXT-ACT-PRV-RECORDER TXT-ACT-PRV-CAMERA TXT-ACT-PRV-TERMINAL TXT-ACT-END-HOS TXT-ACT-END-CHU TXT-ACT-END-MIN TXT-ACT-END-INS",
	"TXT-EVIDENCE-SUP TXT-EVIDENCE-RES TXT-EVIDENCE-UTL TXT-EVIDENCE-PRV TXT-REASON-GROUNDED TXT-REASON-PLATE TXT-REASON-THREAT TXT-REASON-BUSY TXT-REASON-PUSH TXT-REASON-REFILL TXT-REASON-ARM TXT-REASON-THROW-LIMB TXT-REASON-REMOVE TXT-REASON-REPLACE TXT-REASON-REGEN TXT-REASON-RES-LATCH TXT-REASON-RES-WAIT TXT-REASON-RESET TXT-REASON-EVIDENCE",
	"TXT-GATE-HOS TXT-GATE-CHU TXT-GATE-MIN TXT-GATE-INS TXT-GATE-FINAL TXT-GATE-PERMIT TXT-GATE-CONSENT TXT-BOSS-PROTECTION TXT-BOSS-EXPOSED TXT-BOSS-RELAY-A TXT-BOSS-RELAY-B TXT-INCIDENT-WASTE TXT-INCIDENT-ASSAULT TXT-INCIDENT-FAILED TXT-REC-UNCLEAN TXT-REC-OLDER-LOSS TXT-REPLAY-TABLEAU TXT-AFTER-HOS-END TXT-AFTER-CHU-END TXT-AFTER-MIN-END TXT-AFTER-INS-END",
	"TXT-ANC-PRO-01 TXT-ANC-PRO-02 TXT-ANC-PRO-03 TXT-ANC-MIN-PERMIT TXT-ANC-END-DEATH TXT-ANC-END-HELL TXT-ANC-END-CEREMONY TXT-ANC-END-TABLEAU TXT-END-REVIEW TXT-END-CONSENT TXT-END-DEATH TXT-END-HELL TXT-END-CEREMONY TXT-END-TABLEAU",
}

// Dialogue bindings are independent of wording: a renamed or misplaced unit
// must not silently change who speaks or which scene may select it.
var dialogueBindings = []struct {
	reference string
	speaker   string
	ids       string
}{
	{"NAR-01", "CHR-NIN", "DLG-NAR01-NIN-01 DLG-NAR01-NIN-02 DLG-NAR01-NIN-03"},
	{"NAR-01", "CHR-GRI", "DLG-NAR01-GRI-01 DLG-NAR01-GRI-02"},
	{"NAR-02", "CHR-SAV", "DLG-NAR02-SAV-01 DLG-NAR02-SAV-02"},
	{"NAR-02", "CROWD", "DLG-NAR02-CROWD-01 DLG-NAR02-CROWD-02"},
	{"NAR-02", "CHR-GRI", "DLG-NAR02-GRI-01"},
	{"NAR-03", "CHR-VER", "DLG-NAR03-VER-01 DLG-NAR03-VER-02 DLG-NAR03-VER-03"},
	{"NAR-03", "CHR-GRI", "DLG-NAR03-GRI-01 DLG-NAR03-GRI-02"},
	{"NAR-04", "CHR-NIN", "DLG-NAR04-NIN-01 DLG-NAR04-NIN-02 DLG-NAR04-NIN-03"},
	{"NAR-04", "CHR-GRI", "DLG-NAR04-GRI-01 DLG-HOS-SUP-GRI-01"},
	{"NAR-04", "CHR-POT", "DLG-NAR04-POT-01 DLG-NAR04-POT-02"},
	{"NAR-04", "OBJ-D-HOS-RES-02-ACTOR", "DLG-HOS-RES-PETR-01"},
	{"NAR-05", "CHR-POT", "DLG-HOS-BOSS-POT-01 DLG-NAR05-POT-01 DLG-HOS-END-POT-01 DLG-NAR05-POT-02"},
	{"NAR-05", "CHR-GRI", "DLG-NAR05-GRI-01"},
	{"NAR-05", "CHR-NIN", "DLG-HOS-END-NIN-01 DLG-NAR05-NIN-01"},
	{"NAR-05", "CHR-VER", "DLG-NAR05-VER-01"},
	{"NAR-06", "CHR-SAV", "DLG-CHU-ENTRY-SAV-01 DLG-NAR06-SAV-01 DLG-NAR06-SAV-02 DLG-NAR06-SAV-03 DLG-CHU-END-SAV-01"},
	{"NAR-06", "CHR-GRI", "DLG-CHU-PRV-GRI-01 DLG-NAR06-GRI-01 DLG-NAR06-GRI-02"},
	{"NAR-06", "OBJ-D-CHU-RES-05-ACTOR", "DLG-CHU-RES-OKSANA-01"},
	{"NAR-07", "CHR-VER", "DLG-MIN-ENTRY-VER-01 DLG-NAR07-VER-01 DLG-MIN-END-VER-01"},
	{"NAR-07", "CHR-BLA", "DLG-NAR07-BLA-01 DLG-MIN-BOSS-BLA-01 DLG-NAR07-BLA-02"},
	{"NAR-07", "CHR-GRI", "DLG-MIN-UTL-GRI-01 DLG-NAR07-GRI-01"},
	{"NAR-07", "OBJ-D-MIN-RES-02-ACTOR", "DLG-MIN-RES-VIKTOR-01"},
	{"NAR-08", "CHR-GRI", "DLG-INS-ENTRY-GRI-01 DLG-NAR08-GRI-01"},
	{"NAR-08", "CHR-LEV", "DLG-INS-SOURCE-LEV-01 DLG-NAR08-LEV-01 DLG-NAR08-LEV-02"},
	{"NAR-08", "CHR-VER", "DLG-NAR08-VER-01"},
	{"NAR-08", "OBJ-D-INS-RES-05-ACTOR", "DLG-INS-RES-ELENA-01"},
	{"NAR-09", "CHR-LEV", "DLG-NAR09-LEV-01 DLG-NAR09-LEV-02"},
	{"NAR-09", "CHR-GRI", "DLG-NAR09-GRI-01 DLG-INS-END-GRI-01"},
	{"NAR-09", "CHR-VER", "DLG-NAR09-VER-01 DLG-INS-END-VER-01"},
	{"END-REVIEW", "CHR-VER", "DLG-END-REVIEW-VER-01 DLG-END-REVIEW-VER-02"},
	{"END-REVIEW", "CHR-GRI", "DLG-END-REVIEW-GRI-01"},
	{"END-CONSENT", "CHR-VER", "DLG-END-CONSENT-VER-01"},
	{"END-CONSENT", "CHR-GRI", "DLG-END-CONSENT-GRI-01"},
	{"END-DEATH", "CHR-VER", "DLG-END-DEATH-VER-01"},
	{"END-DEATH", "CHR-GRI", "DLG-END-DEATH-GRI-01"},
	{"END-HELL", "CHR-SAT", "DLG-END-HELL-SAT-01 DLG-END-HELL-SAT-02 DLG-END-HELL-SAT-03 DLG-END-HELL-SAT-04 DLG-END-HELL-SAT-05 DLG-END-HELL-SAT-06 DLG-END-HELL-SAT-07"},
	{"END-HELL", "CHR-GRI", "DLG-END-HELL-GRI-01 DLG-END-HELL-GRI-02"},
	{"END-CEREMONY", "CHR-VER", "DLG-END-CER-VER-01"},
	{"END-CEREMONY", "CHR-GRI", "DLG-END-CER-GRI-01 DLG-END-CER-GRI-02"},
	{"END-CEREMONY", "CHR-NIN", "DLG-END-CER-NIN-01"},
	{"END-CEREMONY", "CHR-POT", "DLG-END-CER-POT-01"},
	{"END-CEREMONY", "CHR-SAV", "DLG-END-CER-SAV-01"},
	{"END-CEREMONY", "CHR-BLA", "DLG-END-CER-BLA-01"},
	{"END-CEREMONY", "CHR-LEV", "DLG-END-CER-LEV-01"},
	{"END-TABLEAU", "CHR-NIN", "DLG-END-TABLEAU-NIN-01"},
}

var institutionNames = map[string]string{
	"HOS": "Больница «Дай Бог здоровья»",

	"CHU": "Церковь Святого Кэшбэка",

	"MIN": "Министерство общественного благополучия",

	"INS": "Институт полезных дебилов",
}

// LoadRussian reads the five authored documents through the content boundary.
// It checks all present references against the established campaign identities;
// future scene files and final media are not required to exist.
func LoadRussian(ctx context.Context, filesystem fs.FS, campaign *Campaign) (_ *RussianText, err error) {
	defer mon.Task()(&ctx)(&err)

	if campaign == nil {
		return nil, ErrInvalid.New("missing campaign for Russian text")
	}
	catalogue := &RussianText{entries: make(map[string]TextEntry)}
	actors := make(map[string]bool)
	for _, object := range campaign.Objects(ctx) {
		if object.Role == RoleActor && object.Deed != "" {
			actors[string(object.ID)] = true
		}
	}
	for fileIndex, source := range textFiles {
		var document textDocument
		if err = readCampaignDocument(ctx, filesystem, source.path, &document); err != nil {
			return nil, err
		}
		if document.SchemaVersion != 1 || len(document.Entries) == 0 {
			return nil, ErrInvalid.New("%s: invalid schema or empty catalogue", source.path)
		}
		for _, entry := range document.Entries {
			if !validID(entry.ID, source.prefix) || !allText(entry.Text) || !utf8.ValidString(entry.Text) {
				return nil, ErrInvalid.New("%s: invalid text entry %q", source.path, entry.ID)
			}
			if _, exists := catalogue.entries[entry.ID]; exists {
				return nil, ErrInvalid.New("duplicate text identity %s", entry.ID)
			}
			if err = validateTextEntry(entry, fileIndex, actors); err != nil {
				return nil, ErrInvalid.Wrap(fmt.Errorf("%s: %w", entry.ID, err))
			}
			catalogue.entries[entry.ID] = entry
			catalogue.ordered = append(catalogue.ordered, entry)
		}
	}
	if err = catalogue.addCampaign(ctx, campaign); err != nil {
		return nil, err
	}
	if err = catalogue.validateCoverage(ctx); err != nil {
		return nil, err
	}
	return catalogue, nil
}

func validateTextEntry(entry TextEntry, fileIndex int, actors map[string]bool) error {
	fields, err := textFields(entry.Text)
	if err != nil {
		return err
	}
	expected := strings.Fields(templateFields[entry.ID])
	if strings.HasPrefix(entry.ID, "TXT-AFTER-") && !strings.HasSuffix(entry.ID, "-END") {
		expected = []string{"service"}
	}
	sort.Strings(expected)
	if strings.Join(fields, " ") != strings.Join(expected, " ") {
		return fmt.Errorf("incorrect named fields: got %v, want %v", fields, expected)
	}
	switch fileIndex {
	case 0:
		if entry.Reference != "" || entry.Speaker != "" {
			return fmt.Errorf("interface text has unexpected reference or speaker")
		}
	case 1:
		if !narrativePattern.MatchString(entry.Reference) && !endingPattern.MatchString(entry.Reference) {
			return fmt.Errorf("unknown narrative reference %q: %w", entry.Reference, ErrMissingReference)
		}
		if (!validTextSpeaker(entry.Speaker) && !actors[entry.Speaker]) || entry.Speaker == "FOLLOWERS" {
			return fmt.Errorf("unknown dialogue speaker %q: %w", entry.Speaker, ErrMissingReference)
		}
	case 2:
		if !encounterPattern.MatchString(entry.Reference) || entry.Speaker != "" {
			return fmt.Errorf("unknown tutorial encounter %q: %w", entry.Reference, ErrMissingReference)
		}
	case 3:
		trend := entry.ID == "CAP-TREND-SIXSEVEN" || entry.ID == "CAP-TREND-LEVEL" || entry.ID == "CAP-TREND-BRAND"
		if entry.Reference != "" || (trend && entry.Speaker != "FOLLOWERS") || (!trend && entry.Speaker != "") {
			return fmt.Errorf("invalid caption binding")
		}
	case 4:
		if (!locationPattern.MatchString(entry.Reference) && !endingPattern.MatchString(entry.Reference)) || entry.Speaker != "" {
			return fmt.Errorf("unknown sign reference %q: %w", entry.Reference, ErrMissingReference)
		}
		if !strings.HasPrefix(entry.ID, "TXT-SIGN-") {
			return fmt.Errorf("not a sign layer")
		}
		for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
			if (entry.ID == "TXT-SIGN-"+code || strings.HasPrefix(entry.ID, "TXT-SIGN-"+code+"-")) && entry.Reference != "LOC-"+code {
				return fmt.Errorf("wrong location for sign %s", entry.ID)
			}
		}
		if (entry.ID == "TXT-SIGN-APPOINTMENT" && entry.Reference != "END-HELL") || (entry.ID == "TXT-SIGN-CASINO" && entry.Reference != "END-CEREMONY") {
			return fmt.Errorf("wrong ending for sign %s", entry.ID)
		}
	}
	return nil
}

func validTextSpeaker(speaker string) bool {
	switch speaker {
	case "CHR-GRI", "CHR-VER", "CHR-NIN", "CHR-POT", "CHR-SAV", "CHR-BLA", "CHR-LEV", "CHR-SAT", "CROWD", "FOLLOWERS":
		return true
	default:
		return false
	}
}

// textFields parses only complete ASCII field names; unmatched or nested braces
// are invalid, including in texts that have no interpolation contract.
func textFields(text string) ([]string, error) {
	fields := make(map[string]bool)
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '}':
			return nil, fmt.Errorf("unmatched closing brace")
		case '{':
			end := strings.IndexByte(text[i+1:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed named field")
			}
			end += i + 1
			name := text[i+1 : end]
			if name == "" {
				return nil, fmt.Errorf("empty named field")
			}
			for _, char := range name {
				if (char < 'a' || char > 'z') && char != '_' {
					return nil, fmt.Errorf("invalid named field %q", name)
				}
			}
			fields[name] = true
			i = end
		}
	}
	result := make([]string, 0, len(fields))
	for field := range fields {
		result = append(result, field)
	}
	sort.Strings(result)
	return result, nil
}

func (catalogue *RussianText) addCampaign(ctx context.Context, campaign *Campaign) (err error) {
	defer mon.Task()(&ctx)(&err)
	deeds := campaign.Deeds(ctx)
	if len(deeds) != 100 {
		return ErrInvalid.New("campaign has %d deeds, expected 100", len(deeds))
	}
	seen := make(map[DeedID]bool)
	for _, deed := range deeds {
		if seen[deed.Deed.ID] || !deedPattern.MatchString(string(deed.Deed.ID)) || !allText(deed.Title) || !utf8.ValidString(deed.Title) {
			return ErrInvalid.New("invalid campaign text for %s", deed.Deed.ID)
		}
		fields, fieldErr := textFields(deed.Title)
		if fieldErr != nil || len(fields) != 0 {
			return ErrInvalid.New("invalid service title fields for %s", deed.Deed.ID)
		}
		seen[deed.Deed.ID] = true
		code := strings.TrimPrefix(string(deed.Location), "LOC-")
		if institutionNames[code] == "" || institutionNames[code] != deed.InstitutionName || !strings.HasPrefix(string(deed.Deed.ID), "D-"+code+"-") {
			return ErrInvalid.New("inconsistent institution for %s", deed.Deed.ID)
		}
		id := "TXT-" + string(deed.Deed.ID)
		if _, exists := catalogue.entries[id]; exists {
			return ErrInvalid.New("duplicate service title %s", id)
		}
		entry := TextEntry{ID: id, Text: deed.Title, Reference: string(deed.Deed.ID)}
		catalogue.entries[id] = entry
		catalogue.ordered = append(catalogue.ordered, entry)
		if deed.Family == "SUP" || deed.Family == "RES" {
			detailID := "TXT-DETAIL-" + string(deed.Deed.ID)
			if _, ok := catalogue.entries[detailID]; !ok {
				return fmt.Errorf("%w: service detail %s", ErrMissingReference, detailID)
			}
		}
		if deed.Family == "RES" {
			actor := "OBJ-" + string(deed.Deed.ID) + "-ACTOR"
			found := false
			for _, object := range deed.Deed.Objects {
				found = found || string(object) == actor
			}
			fields, fieldErr := textFields(deed.Label)
			if !found || !allText(deed.Label) || !utf8.ValidString(deed.Label) || fieldErr != nil || len(fields) != 0 {
				return ErrInvalid.New("invalid rescue actor for %s", deed.Deed.ID)
			}
			actorID := "TXT-" + actor
			if _, exists := catalogue.entries[actorID]; exists {
				return ErrInvalid.New("duplicate actor name %s", actorID)
			}
			name := TextEntry{ID: actorID, Text: deed.Label, Reference: actor}
			catalogue.entries[actorID] = name
			catalogue.ordered = append(catalogue.ordered, name)
		}
	}
	for _, entry := range catalogue.ordered {
		if !strings.HasPrefix(entry.ID, "TXT-DETAIL-") {
			continue
		}
		id := DeedID(strings.TrimPrefix(entry.ID, "TXT-DETAIL-"))
		if !seen[id] || (!strings.Contains(string(id), "-SUP-") && !strings.Contains(string(id), "-RES-")) {
			return fmt.Errorf("%w: service detail %s", ErrMissingReference, entry.ID)
		}
	}
	for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
		id := "TXT-LOC-" + code
		if _, exists := catalogue.entries[id]; exists {
			return ErrInvalid.New("duplicate institution name %s", id)
		}
		entry := TextEntry{ID: id, Text: institutionNames[code], Reference: "LOC-" + code}
		catalogue.entries[id] = entry
		catalogue.ordered = append(catalogue.ordered, entry)
		if sign, ok := catalogue.entries["TXT-SIGN-"+code]; !ok || sign.Text != entry.Text || sign.Reference != entry.Reference {
			return ErrInvalid.New("inconsistent institution sign %s", code)
		}
	}
	return nil
}

func (catalogue *RussianText) validateCoverage(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for _, group := range requiredText {
		for _, id := range strings.Fields(group) {
			if _, ok := catalogue.entries[id]; !ok {
				return fmt.Errorf("%w: required text %s", ErrMissingReference, id)
			}
		}
	}
	for _, binding := range dialogueBindings {
		for _, id := range strings.Fields(binding.ids) {
			entry, ok := catalogue.entries[id]
			if !ok {
				return fmt.Errorf("%w: dialogue %s", ErrMissingReference, id)
			}
			if entry.Reference != binding.reference || entry.Speaker != binding.speaker {
				return fmt.Errorf("%s: wrong dialogue reference or speaker", id)
			}
		}
	}
	for _, binding := range []struct{ reference, ids string }{
		{"ENC-PRO-01", "TUT-MOV TUT-INT"}, {"ENC-PRO-03", "TUT-LED"},
		{"ENC-HOS-01", "TUT-CBT TUT-CRD TUT-REC TUT-REC-DEATH"},
		{"ENC-HOS-02", "TUT-LIM TUT-LIM-PLATE"}, {"ENC-HOS-03", "TUT-UTL"},
		{"ENC-HOS-04", "TUT-FEC TUT-FEC-PRACTICE"}, {"ENC-HOS-05", "TUT-BOSS"},
		{"ENC-INS-01", "TUT-SOURCE"},
	} {
		for _, id := range strings.Fields(binding.ids) {
			if catalogue.entries[id].Reference != binding.reference {
				return fmt.Errorf("%s: wrong tutorial encounter", id)
			}
		}
	}
	for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
		for _, number := range []string{"01", "02", "03", "04", "05", "OUT"} {
			id := "TXT-ANC-" + code + "-" + number
			if _, ok := catalogue.entries[id]; !ok {
				return fmt.Errorf("%w: recovery anchor text %s", ErrMissingReference, id)
			}
		}
		for _, family := range []string{"SUP", "RES", "UTL", "PRV"} {
			for _, prefix := range []string{"TXT-AFTER-", "TXT-DANGER-"} {
				id := prefix + code + "-" + family
				if _, ok := catalogue.entries[id]; !ok {
					return fmt.Errorf("%w: %s", ErrMissingReference, id)
				}
			}
		}
		for _, attack := range []string{"A", "B", "C"} {
			for _, phase := range []string{"PREP", "ACTIVE", "RECOVER"} {
				id := "CAP-BOSS-" + code + "-" + attack + "-" + phase
				if _, ok := catalogue.entries[id]; !ok {
					return fmt.Errorf("%w: %s", ErrMissingReference, id)
				}
			}
		}
	}
	for _, ref := range []string{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09", "END-REVIEW", "END-CONSENT", "END-DEATH", "END-HELL", "END-CEREMONY", "END-TABLEAU"} {
		found := false
		for _, entry := range catalogue.ordered {
			if strings.HasPrefix(entry.ID, "DLG-") && entry.Reference == ref {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: dialogue for %s", ErrMissingReference, ref)
		}
	}
	// These lines are not paraphrasable; their identities and exact punctuation
	// are part of the narrative and recovery contracts.
	for _, required := range []struct{ id, text string }{
		{"DLG-NAR02-SAV-01", "Господь забирает лучших"},
		{"DLG-NAR02-CROWD-01", "А этого нахуя вернули?"},
		{"DLG-END-HELL-SAT-03", "Ебать... Ты вообще конченый."},
		{"DLG-END-HELL-GRI-01", "Так я могу остаться?"},
		{"DLG-END-HELL-SAT-04", "Нет, блядь. У нас тут хоть какие-то стандарты."},
		{"DLG-END-HELL-GRI-02", "И КУДА МНЕ?!"},
		{"DLG-END-HELL-SAT-05", "Есть вакансия. Как раз под тебя."},
		{"TXT-ENDING-TABLEAU", "ПОЗДРАВЛЯЕМ! ВЫ СДЕЛАЛИ МИР ЛУЧШЕ"},
		{"TXT-NOTICE-ADULT", "18+. Кровь, расчленение, испражнения, грубая речь, смерть и жестокая социальная сатира. Есть сцена пожара в детской больнице."},
		{"TXT-DEATH-HEADING", "ТЫ СДОХ, УЁБОК. НАЧИСЛЕН ШТРАФ $49,99"},
		{"TXT-DEATH-PRESERVED", "Зачтённые дела сохранены"},
		{"TXT-RETURN-WARN", "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются"},
		{"TXT-PRO-THEFT", "Украсть пожертвования на лечение рака"},
		{"TXT-CONSENT", "Согласен умереть окончательно"},
		{"TXT-SIGN-HOS-WING", "ДЕТСКОЕ ОТДЕЛЕНИЕ"},
		{"TXT-SIGN-MIN-CONTROL", "Контроль без унижения"},
		{"TXT-SIGN-APPOINTMENT", "Министр здравоохранения"},
		{"TXT-RES-FOLLOW", "Иди за мной"},
		{"TXT-OBJECTIVE-99", "Остановить вещание и восстановить городской узел"},
		{"TXT-OBJECTIVE-100", "Получить разрешение умереть в министерстве"},
		{"TXT-EMERGENCY", "Зачёт сохранён. Опасная зона отмечена"},
		{"TXT-REC-SETTINGS", "Настройки сброшены. Прогресс кампании не изменён."},
		{"TXT-TITLE-SUBTITLE", "Смерть по предварительной оплате"},
		{"TXT-MENU-FOCUS", "Игра на паузе: окно неактивно"},
		{"TXT-MENU-CONTROLLER", "Контроллер отключён. Можно использовать клавиатуру"},
		{"TXT-HUD-NO-LEGS", "Нет прыжка и рывка"},
		{"TXT-HUD-NO-ARMS", "Бросок недоступен. Удар головой доступен"},
		{"TXT-EVD-SUP", "Доставьте тележку на площадку. Не повредите пломбу под прессом."},
		{"TXT-EVD-RES", "Человек ждёт. Откройте безопасный проход и позовите его."},
		{"TXT-EVD-UTL", "Попадите конечностью в рычаг. Потом замкните обход."},
		{"TXT-EVD-PRV", "Сначала включите запись. Потом закройте камеру калом."},
		{"TXT-EVD-READY", "Доказательство готово. Подтвердите услугу у терминала."},
		{"TXT-EVD-FAILED", "Условие нарушено: {reason}. Сбросьте попытку у терминала."},
		{"TXT-EVD-EXISTING", "Услуга уже зачтена. Квитанция доступна в журнале."},
		{"TXT-RECEIPT-HEADING", "ЗАЧТЕНО: +1"},
		{"TXT-CONSEQUENCE-HEADING", "ПОСЛЕДСТВИЯ"},
		{"TXT-ATTEMPT-HEADING", "ПОПЫТКА НЕ ЗАСЧИТАНА"},
		{"TXT-SAVE-PENDING", "Сохранение…"}, {"TXT-SAVE-OK", "Сохранено"},
		{"TXT-SAVE-ERROR", "Не удалось сохранить прогресс"},
		{"TXT-REC-RESET", "Незачтённая работа попытки сброшена"},
		{"TXT-REC-READY", "Восстановление завершено. Продолжить?"},
	} {
		if entry, ok := catalogue.entries[required.id]; !ok || entry.Text != required.text {
			return fmt.Errorf("%s: prescribed wording changed", required.id)
		}
	}
	for _, sequence := range [][]string{
		{"DLG-NAR02-SAV-01", "DLG-NAR02-CROWD-01"},
		{"DLG-END-HELL-SAT-03", "DLG-END-HELL-GRI-01", "DLG-END-HELL-SAT-04", "DLG-END-HELL-GRI-02", "DLG-END-HELL-SAT-05"},
	} {
		next := 0
		for _, entry := range catalogue.ordered {
			if next < len(sequence) && entry.ID == sequence[next] {
				next++
			}
		}
		if next != len(sequence) {
			return fmt.Errorf("dialogue exchange out of order: %s", sequence[next])
		}
	}
	return nil
}

// Entry returns a value copy, preserving authored units and separate sign layers.
func (catalogue *RussianText) Entry(id string) (TextEntry, bool) {
	entry, ok := catalogue.entries[id]
	return entry, ok
}

// ForReference returns authored entries in source order, suitable for building
// player-advanced dialogue units. The returned slice does not alias the catalogue.
func (catalogue *RussianText) ForReference(ctx context.Context, reference string) []TextEntry {
	defer mon.Task()(&ctx)(nil)
	var result []TextEntry
	for _, entry := range catalogue.ordered {
		if entry.Reference == reference {
			result = append(result, entry)
		}
	}
	return result
}

// Render substitutes complete caller-provided forms. Missing, extra, empty or
// unresolved values are rejected; no Russian word fragments are constructed.
func (catalogue *RussianText) Render(ctx context.Context, id string, values map[string]string) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	entry, ok := catalogue.entries[id]
	if !ok {
		return "", fmt.Errorf("%w: text %s", ErrMissingReference, id)
	}
	fields, err := textFields(entry.Text)
	if err != nil {
		return "", ErrInvalid.Wrap(err)
	}
	if len(values) != len(fields) {
		return "", ErrInvalid.New("%s: incorrect field set", id)
	}
	result := entry.Text
	for _, field := range fields {
		value, ok := values[field]
		if !ok || !allText(value) || !utf8.ValidString(value) || strings.ContainsAny(value, "{}") {
			return "", ErrInvalid.New("%s: missing or unresolved field %s", id, field)
		}
		result = strings.ReplaceAll(result, "{"+field+"}", value)
	}
	return result, nil
}

// FormatDollars renders exact integer cents using the Russian decimal comma.
// It handles the full int64 range without negating the minimum value.
func FormatDollars(cents int64) string {
	whole, fraction := cents/100, cents%100
	if whole < 0 {
		whole = -whole
	}
	if fraction < 0 {
		fraction = -fraction
	}
	prefix := "$"
	if cents < 0 {
		prefix = "-$"
	}
	return fmt.Sprintf("%s%d,%02d", prefix, whole, fraction)
}
