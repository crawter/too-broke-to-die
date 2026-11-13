// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func russianFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, source := range textFiles {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(source.path)))
		if err != nil {
			t.Fatal(err)
		}
		files[source.path] = &fstest.MapFile{Data: data}
	}
	return files
}

func russianFixture(t *testing.T) (*RussianText, *Campaign) {
	t.Helper()
	campaign := campaignFixture(t)
	catalogue, err := LoadRussian(context.Background(), russianFiles(t), campaign)
	if err != nil {
		t.Fatal(err)
	}
	return catalogue, campaign
}

func TestRussianTextAnchorsAndNotices(t *testing.T) {
	catalogue, _ := russianFixture(t)
	anchors := [][2]string{
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
		{"TXT-OBJECTIVE-99", "Остановить вещание и восстановить городской узел"},
		{"TXT-OBJECTIVE-100", "Получить разрешение умереть в министерстве"},
		{"TXT-EMERGENCY", "Зачёт сохранён. Опасная зона отмечена"},
		{"TXT-REC-SETTINGS", "Настройки сброшены. Прогресс кампании не изменён."},
		{"TXT-REASON-THROW-LIMB", "Нет конечности для броска. Оторвите или подберите конечность, чтобы держать её в руке."},
	}
	for _, anchor := range anchors {
		entry, ok := catalogue.Entry(anchor[0])
		if !ok || entry.Text != anchor[1] {
			t.Errorf("%s = %q, want %q", anchor[0], entry.Text, anchor[1])
		}
	}
	hell := catalogue.ForReference(context.Background(), "END-HELL")
	order := []string{"DLG-END-HELL-SAT-03", "DLG-END-HELL-GRI-01", "DLG-END-HELL-SAT-04", "DLG-END-HELL-GRI-02", "DLG-END-HELL-SAT-05"}
	index := 0
	for _, entry := range hell {
		if index < len(order) && entry.ID == order[index] {
			index++
		}
	}
	if index != len(order) {
		t.Fatalf("inherited exchange out of order: %+v", hell)
	}
	if len(hell) == 0 {
		t.Fatal("missing Hell units")
	}
	hell[0].Text = "changed"
	if catalogue.ForReference(context.Background(), "END-HELL")[0].Text == "changed" {
		t.Fatal("unit slice aliases catalogue")
	}
}

func TestRussianTextCampaignAndFormatting(t *testing.T) {
	ctx := context.Background()
	catalogue, campaign := russianFixture(t)
	for _, deed := range campaign.Deeds(ctx) {
		entry, ok := catalogue.Entry("TXT-" + string(deed.Deed.ID))
		if !ok || entry.Text != deed.Title || entry.Reference != string(deed.Deed.ID) {
			t.Errorf("service title %s: %+v", deed.Deed.ID, entry)
		}
		if deed.Family == "SUP" || deed.Family == "RES" {
			detail, ok := catalogue.Entry("TXT-DETAIL-" + string(deed.Deed.ID))
			if !ok || detail.Text == "" {
				t.Errorf("missing Russian detail for %s", deed.Deed.ID)
			}
		}
		if deed.Family == "RES" {
			actor := "OBJ-" + string(deed.Deed.ID) + "-ACTOR"
			name, ok := catalogue.Entry("TXT-" + actor)
			if !ok || name.Text != deed.Label || name.Reference != actor {
				t.Errorf("rescue actor name %s: %+v", actor, name)
			}
		}
		institution, ok := catalogue.Entry("TXT-" + string(deed.Location))
		if !ok || institution.Text != deed.InstitutionName {
			t.Errorf("institution %s: %+v", deed.Location, institution)
		}
	}
	for _, test := range []struct {
		cents int64
		want  string
	}{
		{0, "$0,00"}, {1, "$0,01"}, {999, "$9,99"}, {4999, "$49,99"},
		{-1, "-$0,01"}, {-4999, "-$49,99"}, {math.MaxInt64, "$92233720368547758,07"}, {math.MinInt64, "-$92233720368547758,08"},
	} {
		if got := FormatDollars(test.cents); got != test.want {
			t.Errorf("FormatDollars(%d) = %q, want %q", test.cents, got, test.want)
		}
	}
	service, _ := catalogue.Entry("TXT-D-HOS-RES-02")
	got, err := catalogue.Render(ctx, "TXT-RECEIPT-DETAIL", map[string]string{"service": service.Text, "count": "100"})
	if err != nil || got != "Освободить человека — Пётр, пациент. Зачтено: 100/100" {
		t.Fatalf("receipt: %q, %v", got, err)
	}
	got, err = catalogue.Render(ctx, "TUT-MOV", map[string]string{"move": "левая кнопка мыши / Левый стик", "jump": "Пробел", "down": "Стрелка вниз"})
	if err != nil || !strings.Contains(got, "Левый стик") || !strings.Contains(got, "Пробел") || !strings.Contains(got, "Стрелка вниз") {
		t.Fatalf("resolved controls: %q, %v", got, err)
	}
	got, err = catalogue.Render(ctx, "TXT-REC-OLDER", map[string]string{"anchor": "Длинная контрольная точка у детского отделения", "count": "99", "difference": "разница неизвестна"})
	if err != nil || !strings.Contains(got, "детского отделения") || !strings.Contains(got, "разница неизвестна") {
		t.Fatalf("long Cyrillic recovery: %q, %v", got, err)
	}
	for _, values := range []map[string]string{
		nil, {"service": "тест"}, {"service": "тест", "count": "1", "extra": "x"},
		{"service": "тест", "wrong": "1"}, {"service": "{unresolved}", "count": "1"}, {"service": " ", "count": "1"},
	} {
		if _, err := catalogue.Render(ctx, "TXT-RECEIPT-DETAIL", values); err == nil {
			t.Errorf("accepted fields %v", values)
		}
	}
	if _, err := catalogue.Render(ctx, "TXT-NOT-FOUND", nil); !errors.Is(err, ErrMissingReference) {
		t.Fatalf("missing text identity: %v", err)
	}
	got, err = catalogue.Render(ctx, "TUT-FEC", map[string]string{"select": "LB", "aim": "Правый стик", "throw": "RT", "interact": "X"})
	if err != nil || !strings.Contains(got, "Правый стик") || !strings.Contains(got, "удерживайте X") {
		t.Fatalf("feces tutorial bindings: %q, %v", got, err)
	}
	if _, err := catalogue.Render(ctx, "TUT-FEC", map[string]string{"select": "LB", "throw": "RT", "interact": "X"}); err == nil {
		t.Fatal("accepted unresolved aim binding")
	}
}

func TestRussianTextRejectsMalformedCampaignNames(t *testing.T) {
	for _, test := range []struct {
		name  string
		index int
		field string
		value string
	}{
		{"service placeholder", 0, "title", "Услуга {count}"},
		{"service bad brace", 0, "title", "Услуга {"},
		{"actor placeholder", 6, "label", "Имя {actor}"},
		{"actor invalid UTF-8", 6, "label", string([]byte{0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			campaign := campaignFixture(t)
			if test.field == "title" {
				campaign.deeds[test.index].Title = test.value
			} else {
				campaign.deeds[test.index].Label = test.value
			}
			if _, err := LoadRussian(context.Background(), russianFiles(t), campaign); err == nil {
				t.Fatal("accepted malformed campaign text")
			}
		})
	}
}

func TestRussianTextSceneBindingsAndOrdering(t *testing.T) {
	ctx := context.Background()
	catalogue, _ := russianFixture(t)
	for _, binding := range dialogueBindings {
		for _, id := range strings.Fields(binding.ids) {
			entry, ok := catalogue.Entry(id)
			if !ok || entry.Reference != binding.reference || entry.Speaker != binding.speaker {
				t.Errorf("scene unit %s: %+v", id, entry)
			}
			if strings.HasPrefix(binding.speaker, "OBJ-") {
				if name, ok := catalogue.Entry("TXT-" + binding.speaker); !ok || name.Text == "" {
					t.Errorf("missing named adult for %s", id)
				}
			}
		}
	}
	files := russianFiles(t)
	path := "content/data/ru-dialogue.json"
	text := string(files[path].Data)
	first := `{"id":"DLG-END-HELL-SAT-03","ref":"END-HELL","speaker":"CHR-SAT","text":"Ебать... Ты вообще конченый."}`
	second := `{"id":"DLG-END-HELL-GRI-01","ref":"END-HELL","speaker":"CHR-GRI","text":"Так я могу остаться?"}`
	if !strings.Contains(text, first) || !strings.Contains(text, second) {
		t.Fatal("exchange fixture changed")
	}
	text = strings.Replace(text, first, "EXCHANGE_PLACEHOLDER", 1)
	text = strings.Replace(text, second, first, 1)
	text = strings.Replace(text, "EXCHANGE_PLACEHOLDER", second, 1)
	files[path].Data = []byte(text)
	if _, err := LoadRussian(ctx, files, campaignFixture(t)); err == nil {
		t.Fatal("accepted reversed inherited exchange")
	}
}

func TestRussianTextStrictValidation(t *testing.T) {
	ctx := context.Background()
	campaign := campaignFixture(t)
	interfacePath := "content/data/ru-interface.json"
	dialoguePath := "content/data/ru-dialogue.json"
	signPath := "content/data/ru-signs.json"
	cases := []struct {
		name string
		path string
		old  string
		new  string
	}{
		{"duplicate key", interfacePath, `"schema_version": 1`, `"schema_version": 1, "schema_version": 1`},
		{"unknown key", interfacePath, `"schema_version": 1`, `"schema_version": 1, "unknown": true`},
		{"wrong case", interfacePath, `"schema_version": 1`, `"Schema_version": 1`},
		{"wrong schema", interfacePath, `"schema_version": 1`, `"schema_version": 2`},
		{"null", interfacePath, `"schema_version": 1`, `"schema_version": null`},
		{"duplicate id", interfacePath, `"TXT-MENU-NEW"`, `"TXT-MENU-CONTINUE"`},
		{"missing id", interfacePath, `"TXT-MENU-NEW"`, `"TXT-MENU-RENAMED"`},
		{"empty id", interfacePath, `"TXT-MENU-NEW"`, `""`},
		{"missing required field", interfacePath, `Зачтено: {count}/100`, `Зачтено: 100/100`},
		{"wrong named field", interfacePath, `{count}/100`, `{total}/100`},
		{"unclosed field", interfacePath, `{count}/100`, `{count/100`},
		{"nested field", interfacePath, `{count}/100`, `{{count}}/100`},
		{"unexpected field", interfacePath, `Сохранение…`, `Сохранение {count}…`},
		{"unknown narrative", dialoguePath, `"ref":"NAR-01"`, `"ref":"NAR-99"`},
		{"wrong existing narrative", dialoguePath, `"ref":"NAR-01"`, `"ref":"NAR-02"`},
		{"unknown actor", dialoguePath, `"speaker":"CHR-NIN"`, `"speaker":"CHR-UNKNOWN"`},
		{"wrong existing actor", dialoguePath, `"speaker":"CHR-NIN"`, `"speaker":"CHR-VER"`},
		{"missing dialogue", dialoguePath, `"DLG-NAR01-NIN-01"`, `"DLG-NAR01-NIN-99"`},
		{"missing rescue actor", dialoguePath, `OBJ-D-HOS-RES-02-ACTOR`, `OBJ-D-HOS-RES-07-ACTOR`},
		{"changed anchor", dialoguePath, `Господь забирает лучших`, `Господь забирает хороших`},
		{"changed death notice", interfacePath, `ТЫ СДОХ, УЁБОК`, `ТЫ УМЕР`},
		{"missing anchor label", interfacePath, `"TXT-ANC-INS-04"`, `"TXT-ANC-INS-99"`},
		{"missing closure action", interfacePath, `"TXT-ACT-END-HOS"`, `"TXT-ACT-END-OTHER"`},
		{"missing limb throw guidance", interfacePath, `"TXT-REASON-THROW-LIMB"`, `"TXT-REASON-THROW-OTHER"`},
		{"unknown service detail", interfacePath, `"TXT-DETAIL-D-HOS-SUP-01"`, `"TXT-DETAIL-D-HOS-SUP-07"`},
		{"wrong tutorial encounter", "content/data/ru-tutorials.json", `"ref":"ENC-PRO-01"`, `"ref":"ENC-PRO-02"`},
		{"missing chant speaker", "content/data/ru-captions.json", `"speaker":"FOLLOWERS",`, ``},
		{"inconsistent sign", signPath, `Больница «Дай Бог здоровья»`, `Другая больница`},
		{"bad sign reference", signPath, `"ref":"LOC-HOS"`, `"ref":"LOC-XYZ"`},
		{"wrong existing sign location", signPath, `"ref":"LOC-HOS"`, `"ref":"LOC-CHU"`},
		{"bad unicode escape", interfacePath, `Смерть по предварительной оплате`, `\ud800`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			files := russianFiles(t)
			original := string(files[test.path].Data)
			if !strings.Contains(original, test.old) {
				t.Fatalf("fixture does not contain %q", test.old)
			}
			files[test.path].Data = []byte(strings.Replace(original, test.old, test.new, 1))
			if _, err := LoadRussian(ctx, files, campaign); err == nil {
				t.Fatal("invalid catalogue accepted")
			}
		})
	}
	files := russianFiles(t)
	files[interfacePath].Data = append(files[interfacePath].Data, []byte(` {}`)...)
	if _, err := LoadRussian(ctx, files, campaign); err == nil {
		t.Fatal("trailing value accepted")
	}
	files = russianFiles(t)
	files[interfacePath].Data = append(files[interfacePath].Data, 0xff)
	if _, err := LoadRussian(ctx, files, campaign); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	files = russianFiles(t)
	delete(files, dialoguePath)
	if _, err := LoadRussian(ctx, files, campaign); err == nil {
		t.Fatal("missing document accepted")
	}
	if _, err := LoadRussian(ctx, russianFiles(t), nil); err == nil {
		t.Fatal("missing campaign accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := LoadRussian(cancelled, russianFiles(t), campaign); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load: %v", err)
	}
}
