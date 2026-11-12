// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const campaignPath = "content/data/campaign.json"

const servicesPath = "content/data/ru-services.json"

func campaignFixture(t *testing.T) *Campaign {
	t.Helper()
	campaign, err := LoadCampaign(context.Background(), os.DirFS("../../.."), campaignPath, servicesPath)
	if err != nil {
		t.Fatal(err)
	}
	return campaign
}

func TestCampaignManifest(t *testing.T) {
	campaign := campaignFixture(t)
	ctx := context.Background()
	deeds := campaign.Deeds(ctx)
	batches := campaign.Batches(ctx)
	if len(deeds) != 100 || len(batches) != 16 {
		t.Fatalf("got %d deeds, %d batches", len(deeds), len(batches))
	}
	// Section 8.5 labels are kept independently of the authored data under test.
	labels := []string{
		"Перевязочная А|Палата Б|Диализ|Реабилитация|Кислородный пост|Приёмный покой",
		"Мария, санитарка|Пётр, пациент|Алла, техник|Борис, пациент|Зоя, медсестра|Семён, уборщик",
		"Вода перевязочной|Свет палаты Б|Насос диализа|Лифт реабилитации|Вентиляция склада|Сигнал приёмного покоя",
		"Камера переодевания|Камера палаты Б|Экран диализа|Камера реабилитации|Камера кислородного поста|Камера сбора пожертвований",
		"Столовая|Ночлег|Пункт одежды|Похоронное бюро|Исповедальни|Благотворительный склад",
		"Тамара, волонтёр|Илья, постоялец|Людмила, швея|Павел, гробовщик|Оксана, прихожанка|Денис, архивист",
		"Холодильник столовой|Свет ночлега|Обогрев пункта одежды|Лифт похоронного бюро|Вентиляция исповедален|Звонок благотворительного склада",
		"Камера очереди за едой|Камера ночлега|Камера примерочной|Камера похорон|Камера исповеди|Камера списка должников",
		"Зал ожидания|Рабочий архив|Общественный туалет|Бюро пособий|Статистический отдел|Разрешительный стол",
		"Галина, заявительница|Виктор, архивист|Роза, уборщица|Михаил, заявитель|Эмма, статистик|Анатолий, курьер",
		"Вентиляция очереди|Свет рабочего архива|Смыв общественного туалета|Табло пособий|Принтер статистики|Связь разрешительного стола",
		"Камера заявлений|Камера рабочего архива|Камера счётчика отходов|Камера получателей пособий|Камера исправления данных|Камера реестра смерти",
		"Пункт возвратов|Комната отдыха|Юридический стол|Склад реквизита|Пункт выхода|Городской узел",
		"Лидия, слушательница|Артём, монтажёр|Светлана, заявительница|Олег, рабочий сцены|Елена, подписчица|Фёдор, связист",
		"Табло возвратов|Свет комнаты отдыха|Принтер жалоб|Тормоз реквизита|Указатель выхода|Питание городского узла",
		"Камера возвратов|Камера комнаты отдыха|Камера жалоб|Камера склада реквизита|Камера отказа от подписки|Камера городского узла",
	}
	institutions := []string{"HOS", "CHU", "MIN", "INS"}
	names := []string{"Больница «Дай Бог здоровья»", "Церковь Святого Кэшбэка", "Министерство общественного благополучия", "Институт полезных дебилов"}
	families := []string{"SUP", "RES", "UTL", "PRV"}
	titles := []string{"Доставить помощь", "Освободить человека", "Восстановить подачу", "Закрыть слежку"}
	objects := map[ObjectID]Object{}
	for _, object := range campaign.Objects(ctx) {
		if _, duplicate := objects[object.ID]; duplicate {
			t.Fatalf("duplicate object %s", object.ID)
		}
		objects[object.ID] = object
	}
	consequences := map[ConsequenceID]DeedID{}
	for _, consequence := range campaign.Consequences(ctx) {
		if _, duplicate := consequences[consequence.ID]; duplicate {
			t.Fatalf("duplicate consequence %s", consequence.ID)
		}
		consequences[consequence.ID] = consequence.Deed
	}
	if len(consequences) != 100 {
		t.Fatalf("expected 100 consequences, got %d", len(consequences))
	}
	for _, entry := range deeds {
		if consequences[entry.Deed.Consequence] != entry.Deed.ID {
			t.Fatalf("bad consequence for %s", entry.Deed.ID)
		}
		if err := validateServiceObjects(entry.Deed, entry.Family, objects); err != nil {
			t.Fatal(err)
		}
		for _, id := range entry.Deed.Objects {
			object, ok := objects[id]
			if !ok || object.Encounter != entry.Deed.Encounter || (object.Deed != "" && object.Deed != entry.Deed.ID) {
				t.Fatalf("invalid object %s for %s", id, entry.Deed.ID)
			}
		}
	}
	seen := map[DeedID]bool{}
	for i, batch := range batches {
		institution := institutions[i/4]
		family := families[i%4]
		row := strings.Split(labels[i], "|")
		if len(batch.Deeds) != 6 || batch.Emergency.ID != EmergencyID("EM-"+institution+"-"+family) || batch.Emergency.WarningMilliseconds != 1500 || batch.Emergency.ActiveMilliseconds != 8000 {
			t.Fatalf("invalid batch %+v", batch)
		}
		for n, id := range batch.Deeds {
			expectedID := DeedID(fmt.Sprintf("D-%s-%s-%02d", institution, family, n+1))
			deed := deeds[(i/4)*25+(i%4)*6+n]
			if id != expectedID || deed.Deed.ID != id || seen[id] {
				t.Fatalf("invalid or duplicate id %s", id)
			}
			seen[id] = true
			if deed.Label != row[n] || deed.Title != titles[i%4]+" — "+row[n] || deed.InstitutionName != names[i/4] || deed.FamilyID != "FAM-"+family || batch.FamilyID != deed.FamilyID || deed.Deed.Consequence != ConsequenceID("CON-"+string(id)) || deed.Emergency != batch.Emergency.ID || deed.Variant != n+1 {
				t.Errorf("invalid service %+v", deed)
			}
			if deed.Deed.Encounter != batch.Emergency.Encounter || deed.Deed.Anchor != batch.Emergency.Anchor || len(deed.Deed.Objects) == 0 || deed.Evidence == "" || deed.Failure == "" || deed.Reset == "" || deed.Trigger == "" || deed.IndividualAftermath != batch.IndividualAftermath {
				t.Errorf("missing bindings for %s", id)
			}
		}
	}
	for i, institution := range institutions {
		closure := deeds[i*25+24]
		id := DeedID("D-" + institution + "-END")
		if closure.Deed.ID != id || seen[id] || closure.Deed.Anchor != AnchorID("ANC-"+institution+"-OUT") || closure.Deed.Consequence != ConsequenceID("CON-"+string(id)) || !reflect.DeepEqual(closure.Deed.Defeated, []EncounterID{EncounterID("ENC-" + institution + "-05")}) {
			t.Fatalf("bad closure %+v", closure)
		}
		seen[id] = true
	}
	if len(seen) != 100 {
		t.Fatalf("expected exactly 100 distinct sources, got %d", len(seen))
	}
	last := deeds[99]
	if len(last.Deed.Prerequisites) != 99 || last.Deed.HoldMilliseconds != 1500 || last.CompletedNarrative != "NAR-09" || !reflect.DeepEqual(last.InstalledPrerequisites, []DeedID{"D-INS-SUP-06", "D-INS-UTL-06"}) {
		t.Fatalf("bad final closure %+v", last)
	}
}

func TestCampaignGatesAndReverseOrder(t *testing.T) {
	campaign := campaignFixture(t)
	ctx := context.Background()
	progress := CampaignProgress{Narratives: []NarrativeID{"NAR-03"}}
	deeds := campaign.Deeds(ctx)
	for inst := 0; inst < 4; inst++ {
		for family := 0; family < 4; family++ {
			batch := campaign.Batches(ctx)[inst*4+family]
			if family < 3 {
				next := campaign.Batches(ctx)[inst*4+family+1].Deeds[0]
				if ok, err := campaign.CanCertify(ctx, next, progress); err != nil || ok {
					t.Fatalf("next batch opened early: %v %v", ok, err)
				}
			}
			for n := 5; n >= 0; n-- {
				id := batch.Deeds[n]
				ok, err := campaign.CanCertify(ctx, id, progress)
				if err != nil || !ok {
					t.Fatalf("%s unavailable: %v", id, err)
				}
				emergency, err := campaign.SixthEmergency(ctx, id, progress)
				if err != nil || (n == 0 && emergency != batch.Emergency.ID) || (n != 0 && emergency != "") {
					t.Fatalf("%s: emergency %s, %v", id, emergency, err)
				}
				progress.Certified = append(progress.Certified, id)
				if ok, err := campaign.CanCertify(ctx, id, progress); err != nil || ok {
					t.Fatalf("duplicate certification allowed: %v %v", ok, err)
				}
			}
		}
		closure := deeds[inst*25+24].Deed
		if len(progress.Certified) != inst*25+24 {
			t.Fatal("wrong running total")
		}
		if ok, err := campaign.CanCertify(ctx, closure.ID, progress); err != nil || ok {
			t.Fatalf("closure without boss: %v %v", ok, err)
		}
		progress.Defeated = append(progress.Defeated, closure.Encounter)
		if ok, err := campaign.CanCertify(ctx, closure.ID, progress); err != nil || !ok {
			t.Fatalf("closure after boss: %v %v", ok, err)
		}
		if inst == 3 {
			if len(progress.Certified) != 99 {
				t.Fatal("final closure not reserved for 100")
			}
			if ok, _ := campaign.GateOpen(ctx, "G-PERMIT", progress); ok {
				t.Fatal("permit at 99")
			}
			if ok, err := campaign.GateOpen(ctx, "G-INS-FINAL", progress); err != nil || !ok {
				t.Fatalf("final gate: %v %v", ok, err)
			}
		}
		progress.Certified = append(progress.Certified, closure.ID)
		if len(progress.Certified) != (inst+1)*25 {
			t.Fatal("incorrect institution contribution")
		}
	}
	if ok, err := campaign.GateOpen(ctx, "G-PERMIT", progress); err != nil || ok {
		t.Fatalf("permit without NAR-09: %v %v", ok, err)
	}
	progress.Narratives = append(progress.Narratives, "NAR-09")
	if ok, err := campaign.GateOpen(ctx, "G-PERMIT", progress); err != nil || !ok {
		t.Fatalf("permit after 100 and NAR-09: %v %v", ok, err)
	}
	if ok, _ := campaign.GateOpen(ctx, "G-DEATH", progress); ok {
		t.Fatal("death gate without permit and consent")
	}
	progress.Permit, progress.Consent = true, true
	if ok, err := campaign.GateOpen(ctx, "G-DEATH", progress); err != nil || !ok {
		t.Fatalf("death gate: %v %v", ok, err)
	}
	progress.Certified = append(progress.Certified, progress.Certified[0])
	if _, err := campaign.GateOpen(ctx, "G-PERMIT", progress); err == nil {
		t.Fatal("accepted duplicate receipt")
	}
}

func TestCampaignMissingPrerequisites(t *testing.T) {
	campaign := campaignFixture(t)
	ctx := context.Background()
	deeds := campaign.Deeds(ctx)
	progress := CampaignProgress{Narratives: []NarrativeID{"NAR-03", "NAR-09"}, Defeated: []EncounterID{"ENC-HOS-05", "ENC-CHU-05", "ENC-MIN-05", "ENC-INS-05"}}
	for _, deed := range deeds[:99] {
		progress.Certified = append(progress.Certified, deed.Deed.ID)
	}
	for missing := range progress.Certified {
		partial := progress
		partial.Certified = append([]DeedID(nil), progress.Certified[:missing]...)
		partial.Certified = append(partial.Certified, progress.Certified[missing+1:]...)
		if ok, err := campaign.CanCertify(ctx, "D-INS-END", partial); err != nil || ok {
			t.Fatalf("final closure with %s missing: %v %v", progress.Certified[missing], ok, err)
		}
		if ok, err := campaign.GateOpen(ctx, "G-INS-FINAL", partial); err != nil || ok {
			t.Fatalf("final gate with %s missing: %v %v", progress.Certified[missing], ok, err)
		}
	}
	withoutBoss := progress
	withoutBoss.Defeated = []EncounterID{"ENC-HOS-05", "ENC-CHU-05", "ENC-MIN-05"}
	if ok, err := campaign.GateOpen(ctx, "G-INS-FINAL", withoutBoss); err != nil || ok {
		t.Fatalf("final gate without boss: %v %v", ok, err)
	}
	progress.Certified = append(progress.Certified, "D-INS-END")
	for missing := range progress.Certified {
		partial := progress
		partial.Certified = append([]DeedID(nil), progress.Certified[:missing]...)
		partial.Certified = append(partial.Certified, progress.Certified[missing+1:]...)
		if ok, err := campaign.GateOpen(ctx, "G-PERMIT", partial); err != nil || ok {
			t.Fatalf("permit with %s missing: %v %v", progress.Certified[missing], ok, err)
		}
	}
	initial := CampaignProgress{}
	if ok, err := campaign.CanCertify(ctx, "D-HOS-SUP-01", initial); err != nil || ok {
		t.Fatalf("hospital opened before NAR-03: %v %v", ok, err)
	}
	for _, id := range []DeedID{"D-CHU-SUP-01", "D-MIN-SUP-01", "D-INS-SUP-01"} {
		if ok, err := campaign.CanCertify(ctx, id, initial); err != nil || ok {
			t.Fatalf("institution opened early for %s: %v %v", id, ok, err)
		}
	}
}

func TestCampaignInvalidAndIsolation(t *testing.T) {
	root := os.DirFS("../../..")
	campaignData, err := fs.ReadFile(root, campaignPath)
	if err != nil {
		t.Fatal(err)
	}
	textData, err := fs.ReadFile(root, servicesPath)
	if err != nil {
		t.Fatal(err)
	}
	var source campaignSource
	var text serviceTextSource
	if err := json.Unmarshal(campaignData, &source); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(textData, &text); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string

		change func(*campaignSource, *serviceTextSource)
	}{
		{"duplicate ticket", func(_ *campaignSource, v *serviceTextSource) {
			v.Institutions[0].Tickets[1].ID = v.Institutions[0].Tickets[0].ID
		}},
		{"missing ticket", func(_ *campaignSource, v *serviceTextSource) {
			v.Institutions[0].Tickets = v.Institutions[0].Tickets[:23]
		}},
		{"extra credit", func(_ *campaignSource, v *serviceTextSource) {
			v.Institutions[0].Tickets = append(v.Institutions[0].Tickets, serviceText{ID: "D-HOS-SUP-07", Label: "extra"})
		}},
		{"wrong emergency reference", func(v *campaignSource, _ *serviceTextSource) {
			v.Institutions[0].Batches[0].Emergency = "EM-CHU-SUP"
		}},
		{"missing object role", func(v *campaignSource, _ *serviceTextSource) {
			v.Families[0].TicketRoles = nil
		}},
		{"wrong closure", func(v *campaignSource, _ *serviceTextSource) {
			v.Institutions[3].Closure = "D-INS-OTHER"
		}},
		{"missing adult", func(_ *campaignSource, v *serviceTextSource) {
			v.Institutions[0].Tickets[6].Label = ""
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var input campaignSource
			var stringsInput serviceTextSource
			if err := json.Unmarshal(campaignData, &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(textData, &stringsInput); err != nil {
				t.Fatal(err)
			}
			tc.change(&input, &stringsInput)
			if _, err := expandCampaign(context.Background(), input, stringsInput); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("accepted invalid catalogue: %v", err)
			}
		})
	}
	first, err := expandCampaign(context.Background(), source, text)
	if err != nil {
		t.Fatal(err)
	}
	second, err := expandCampaign(context.Background(), source, text)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("nondeterministic expansion: %v", err)
	}
	deeds := first.Deeds(context.Background())
	deeds[0].Deed.Objects[0] = "OBJ-CORRUPT"
	batches := first.Batches(context.Background())
	batches[0].Deeds[0] = "D-CORRUPT"
	gates := first.Gates(context.Background())
	gates[0].Narratives[0] = "NAR-01"
	if !reflect.DeepEqual(first, second) {
		t.Fatal("caller mutated catalogue")
	}
	for _, data := range []string{`{"schema_version":1,"schema_version":1}`, `{"SCHEMA_VERSION":1}`, `null`, `{}` + ` {}`} {
		files := fstest.MapFS{"campaign.json": &fstest.MapFile{Data: []byte(data)}, "text.json": &fstest.MapFile{Data: textData}}
		if _, err := LoadCampaign(context.Background(), files, "campaign.json", "text.json"); err == nil {
			t.Fatalf("accepted invalid JSON %s", data)
		}
	}
	if _, err := LoadCampaign(context.Background(), root, "missing", servicesPath); err == nil {
		t.Fatal("missing document accepted")
	}
	if _, err := first.CanCertify(context.Background(), "D-HOS-SUP-07", CampaignProgress{}); err == nil {
		t.Fatal("accepted non-manifest credit")
	}
}
