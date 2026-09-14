package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Этап 0.10, К32: план как файл обязателен на каждом входе бинарника — петля (уже
// проверено goals_test.go/TestПетляНеИсполняетПланБезЦелей), судья, двигатель, отчёт.
// Три случая ниже — ровно те, что называет шаг 63: продукт без плана, продукт с негодным
// планом, путь плана из run-config.json.

// captureStdout — cmdJudge/cmdDrift/cmdReport печатают отказ через fmt.Print в os.Stdout,
// а не возвращают текст: перехват нужен, чтобы проверить, что путь плана в сообщении
// назван, а не только что код возврата 2.
func captureStdout(t *testing.T, f func() int) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	code := f()
	_ = w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), code
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// minimalRunConfig — судья с одной безобидной проверкой, достаточной, чтобы критерий К1
// плана ниже находил, чем ему меряться (criteriaBinding).
const minimalRunConfig = `{"judge":{"checks":[{"name":"тесты","command":"cmd","args":["/c","exit","0"]}]},"ladder":["script","sonnet"]}`

// minimalGoodPlan — годный план: цель, критерий, привязанный к проверке из
// minimalRunConfig, один исполняемый шаг со ссылкой на критерий и гейт ЛПР.
const minimalGoodPlan = "**Ц1.** план виден по имени из run-config.json\n\n" +
	"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
	"| К1 | Ц1 | проверка тестов проходит | проверка `тесты` |\n\n" +
	"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
	"| 1 | работа | `sonnet` | К1: тест |\n" +
	"| 2 | решение ЛПР | — | гейт: ЛПР |\n"

// planWithoutGoalsBlock — шаги разбираются, а блока целей нет вовсе: пример «плана с
// негодным блоком целей», а не «плана нет».
const planWithoutGoalsBlock = "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | работа | `sonnet` | тест |\n"

// minimalGoodPlanClosed — тот же план, что minimalGoodPlan, но единственный исполняемый
// шаг уже закрыт. Годится ТОЛЬКО для контрольного теста ниже: с открытым шагом (как в
// minimalGoodPlan) двигатель на согласном судье законно отвечает не ALLOW, а «ЖДЁТ ЛПР»
// (WORKER-DRIFT-04, drift.go) — судья доволен, а план говорит иное, — и это не имеет
// отношения к правке шага 63: правило старше её и проверено отдельно в drift_test.go
// (TestСудьяДоволенАПланГоворитИное).
const minimalGoodPlanClosed = "**Ц1.** план виден по имени из run-config.json\n\n" +
	"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
	"| К1 | Ц1 | проверка тестов проходит | проверка `тесты` |\n\n" +
	"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
	"| ~~1~~ | работа | `sonnet` | К1: тест |\n"

// --- 1. продукт без плана --------------------------------------------------------------

func TestПродуктБезПланаСудьяДвигательОтчётОтказываютКодом2(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "run-config.json"), minimalRunConfig)
	wantPath := filepath.Join(dir, "PLAN.md")

	jOut, jCode := captureStdout(t, func() int { return cmdJudge([]string{"-product", dir}) })
	if jCode != 2 {
		t.Errorf("судья на продукте без плана: ожидался код 2, получен %d (%s)", jCode, jOut)
	}
	if !strings.Contains(jOut, wantPath) {
		t.Errorf("отказ судьи не называет путь плана %q:\n%s", wantPath, jOut)
	}

	dOut, dCode := captureStdout(t, func() int { return cmdDrift([]string{"-product", dir}) })
	if dCode != 2 {
		t.Errorf("двигатель на продукте без плана: ожидался код 2, получен %d (%s)", dCode, dOut)
	}
	if !strings.Contains(dOut, wantPath) {
		t.Errorf("отказ двигателя не называет путь плана %q:\n%s", wantPath, dOut)
	}

	rOut, rCode := captureStdout(t, func() int { return cmdReport([]string{"-product", dir}) })
	if rCode != 2 {
		t.Errorf("отчёт на продукте без плана: ожидался код 2, получен %d (%s)", rCode, rOut)
	}
	if !strings.Contains(rOut, wantPath) {
		t.Errorf("отказ отчёта не называет путь плана %q:\n%s", wantPath, rOut)
	}
}

// --- 2. продукт с негодным планом (файл есть, блока целей нет) -------------------------

func TestПродуктСНегоднымПланомСудьяДвигательОтчётОтказываютКодом2(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "run-config.json"), minimalRunConfig)
	planPath := filepath.Join(dir, "PLAN.md")
	mustWriteFile(t, planPath, planWithoutGoalsBlock)

	if code := cmdJudge([]string{"-product", dir}); code != 2 {
		t.Errorf("судья на негодном плане: ожидался код 2, получен %d", code)
	}
	if code := cmdDrift([]string{"-product", dir}); code != 2 {
		t.Errorf("двигатель на негодном плане: ожидался код 2, получен %d", code)
	}
	out, code := captureStdout(t, func() int { return cmdReport([]string{"-product", dir}) })
	if code != 2 {
		t.Errorf("отчёт на негодном плане: ожидался код 2, получен %d (%s)", code, out)
	}
	if !strings.Contains(out, planPath) {
		t.Errorf("отказ отчёта не называет путь плана %q:\n%s", planPath, out)
	}
}

// --- 3. путь плана берётся из run-config.json, а не прибит ------------------------------

func TestПутьПланаБерётсяИзRunConfigНеПрибит(t *testing.T) {
	dir := t.TempDir()
	cfgText := `{"judge":{"checks":[{"name":"тесты","command":"cmd","args":["/c","exit","0"]}]},` +
		`"plan":"CUSTOM-PLAN.md","ladder":["script","sonnet"]}`
	mustWriteFile(t, filepath.Join(dir, "run-config.json"), cfgText)
	customPath := filepath.Join(dir, "CUSTOM-PLAN.md")
	mustWriteFile(t, customPath, minimalGoodPlan)
	// ПРИБИТОГО PLAN.md НЕТ ВОВСЕ: если чтение путём пойдёт мимо run-config.json (прежний
	// дефект двигателя и отчёта), план окажется «не найден», а не «найден по своему имени».

	var cfg runConfig
	if err := readJSON(filepath.Join(dir, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	res := checkPlanRequirement(dir, cfg)
	if res.Path != customPath {
		t.Fatalf("planFilePath не взял имя из run-config.json: получено %q, ожидался %q", res.Path, customPath)
	}
	if !res.OK() {
		t.Fatalf("план по имени из run-config.json объявлен негодным: %v", res.Problems)
	}

	// Сквозным вызовом: двигатель на продукте, где нет файла PLAN.md вовсе, а есть только
	// именованный run-config.json, обязан УВИДЕТЬ план, а не отказать «план не найден».
	dOut, dCode := captureStdout(t, func() int { return cmdDrift([]string{"-product", dir, "-json"}) })
	if dCode == 2 && strings.Contains(dOut, "план не годен") {
		t.Fatalf("двигатель не нашёл план по имени из run-config.json:\n%s", dOut)
	}
	var dm driftMeasure
	if err := json.Unmarshal([]byte(dOut), &dm); err != nil {
		t.Fatalf("вывод двигателя не разобран как JSON: %v\n%s", err, dOut)
	}
	if dm.PlanOpenSteps == nil || *dm.PlanOpenSteps != 1 {
		t.Errorf("двигатель прочитал не тот файл плана: открытых шагов ожидался 1, получено %v", dm.PlanOpenSteps)
	}

	// И судья: та же проверка тем же путём, не «нечем проверить» из-за якобы отсутствующего плана.
	jOut, jCode := captureStdout(t, func() int { return cmdJudge([]string{"-product", dir}) })
	if jCode == 2 && strings.Contains(jOut, "план не годен") {
		t.Fatalf("судья не нашёл план по имени из run-config.json:\n%s", jOut)
	}
}

// --- контроль: годный план на умолчательном месте не мешает ни одному входу -------------

func TestГодныйПланНеМешаетСудьеДвигателюИОтчёту(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "run-config.json"), minimalRunConfig)
	mustWriteFile(t, filepath.Join(dir, "PLAN.md"), minimalGoodPlanClosed)

	if code := cmdJudge([]string{"-product", dir}); code != 0 {
		t.Errorf("судья на годном плане: ожидался код 0 (проверка «тесты» проходит), получен %d", code)
	}
	if code := cmdDrift([]string{"-product", dir}); code != 0 {
		t.Errorf("двигатель на годном плане: ожидался код 0 (ALLOW), получен %d", code)
	}
	if code := cmdReport([]string{"-product", dir}); code != 0 {
		t.Errorf("отчёт на годном плане: ожидался код судьи 0, получен %d", code)
	}
}
