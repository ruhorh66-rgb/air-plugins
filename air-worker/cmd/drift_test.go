package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Тесты проверяют ПРАВИЛА, а не прогон на живом продукте. Прогон доказывает, что сегодня
// на этой машине получилось; тест доказывает, что правило такое, каким объявлено.

func d(v int) *int { return &v }

// pt — замер только с остатком по судье; nil — неизмеримый замер.
func pt(judge any) driftPoint {
	if judge == nil {
		return driftPoint{}
	}
	return driftPoint{Judge: d(judge.(int))}
}

// jOnly — история из замеров только с остатком по судье.
func jOnly(vals ...any) []driftPoint {
	out := make([]driftPoint, 0, len(vals))
	for _, v := range vals {
		out = append(out, pt(v))
	}
	return out
}

// pp — замер с планом: остаток по судье, закрытых шагов, открытых шагов.
func pp(judge, closed, open int) driftPoint {
	return driftPoint{Judge: d(judge), Closed: d(closed), Open: d(open)}
}

func hasRule(rs []driftReason, rule string) bool {
	for _, r := range rs {
		if r.Rule == rule {
			return true
		}
	}
	return false
}

func TestЛестницаЗастоя(t *testing.T) {
	lim := defaultLimits()
	// Остаток 4 не двигается. Порог торможения 3, эскалации 6.
	cases := []struct {
		history []driftPoint
		want    string
	}{
		{jOnly(), verdictAllow},                    // первый замер: сравнивать не с чем
		{jOnly(4), verdictAllow},                   // застой 1
		{jOnly(4, 4), verdictAllow},                // застой 2
		{jOnly(4, 4, 4), verdictThrottle},          // застой 3 — порог торможения
		{jOnly(4, 4, 4, 4, 4), verdictThrottle},    // застой 5
		{jOnly(4, 4, 4, 4, 4, 4), verdictEscalate}, // застой 6 — порог эскалации
	}
	for i, c := range cases {
		got, _ := evaluate(pt(4), d(1), nil, c.history, lim)
		if got != c.want {
			t.Errorf("случай %d: история %d замеров -> %q, ожидалось %q", i, len(c.history), got, c.want)
		}
	}
}

func TestДвижениеСнимаетТормозСразу(t *testing.T) {
	// Пять замеров без движения — и шестой с уменьшением. Тормоз обязан сняться тем же
	// ходом, а не «отстояться»: иначе механизм наказывал бы за уже исправленное.
	got, _ := evaluate(pt(3), d(1), nil, jOnly(5, 5, 5, 5, 5), defaultLimits())
	if got != verdictAllow {
		t.Fatalf("после уменьшения остатка ожидался %q, получено %q", verdictAllow, got)
	}
}

func TestНеизмеримоеНеРавноЧистому(t *testing.T) {
	// Судья с кодом 2 даёт остаток nil. Два таких подряд — эскалация, а не «чисто».
	if got, _ := evaluate(pt(nil), d(2), nil, jOnly(), defaultLimits()); got != verdictAllow {
		t.Errorf("один слепой замер: ожидался %q, получено %q", verdictAllow, got)
	}
	if got, _ := evaluate(pt(nil), d(2), nil, jOnly(nil), defaultLimits()); got != verdictEscalate {
		t.Errorf("два слепых замера подряд: ожидался %q, получено %q", verdictEscalate, got)
	}
}

func TestНулевоеРасстояниеНеЗастаивается(t *testing.T) {
	// Расстояние ноль не уменьшается НИКОГДА. Без этого правила продукт с исчерпанной
	// работой получал бы эскалацию за то, что работа кончилась. Найдено AIR-ENV-002.
	long := jOnly(0, 0, 0, 0, 0, 0, 0, 0)
	got, _ := evaluate(pt(0), d(1), nil, long, defaultLimits())
	if got != verdictBlocked {
		t.Fatalf("восемь замеров при нуле: ожидался %q, получено %q", verdictBlocked, got)
	}
	got, _ = evaluate(pt(0), d(0), nil, long, defaultLimits())
	if got != verdictAllow {
		t.Fatalf("ноль при согласном судье: ожидался %q, получено %q", verdictAllow, got)
	}
}

func TestРостОстаткаТормозитОтдельно(t *testing.T) {
	// Застой — это ноль движения; рост остатка означает, что сделанное разломало закрытое.
	v, reasons := evaluate(pt(5), d(1), nil, jOnly(3), defaultLimits())
	if v != verdictThrottle {
		t.Fatalf("рост остатка по судье: ожидался %q, получено %q", verdictThrottle, v)
	}
	if !hasRule(reasons, "WORKER-DRIFT-03") {
		t.Error("рост остатка должен называться правилом WORKER-DRIFT-03 отдельно от застоя")
	}
}

func TestСтрогостьНеСмягчается(t *testing.T) {
	// Слепота эскалирует, рост тормозит. Порядок правил в коде не должен менять исход:
	// поздний вердикт не затирает раннего.
	if harden(verdictEscalate, verdictThrottle) != verdictEscalate {
		t.Error("торможение не имеет права смягчить эскалацию")
	}
	if harden(verdictAllow, verdictBlocked) != verdictBlocked {
		t.Error("«ждёт ЛПР» строже, чем ALLOW")
	}
}

func TestПорогиПерекрываютсяПродуктом(t *testing.T) {
	lim := defaultLimits().withConfig(driftThresholds{StallThrottle: intPtr(1)})
	if lim.StallThrottle != 1 {
		t.Fatalf("порог торможения не перекрыт: %d", lim.StallThrottle)
	}
	if lim.StallEscalate != 6 {
		t.Errorf("неперекрытый порог обязан остаться зашитым, получено %d", lim.StallEscalate)
	}
}

// --- 14.09.2026: расстояние перестало быть суммой -------------------------------------

func TestЗакрытиеШаговДвижениеПриНеподвижномСудье(t *testing.T) {
	// ASW, замер перед правкой: 24 = 1 + 23. Судья видит план одной проверкой и стоит на
	// единице до последнего шага. Если судить только по нему, восемь честно закрытых
	// шагов подряд дали бы застой 8 и эскалацию на работающем продукте.
	var hist []driftPoint
	for i := 0; i < 8; i++ {
		hist = append(hist, pp(1, i, 23-i))
	}
	cur := pp(1, 8, 15)
	if stall, _ := countStreaks(hist, cur); stall != 0 {
		t.Fatalf("каждый замер закрывал шаг, а застой насчитан %d", stall)
	}
	if v, _ := evaluate(cur, d(1), d(15), hist, defaultLimits()); v != verdictAllow {
		t.Fatalf("закрытие шагов при неподвижном судье: ожидался %q, получено %q", verdictAllow, v)
	}
}

func TestСужениеШагаНеНаказываетсяПервыйРаз(t *testing.T) {
	// AIR-ENV-002: THROTTLE велит сузить шаг; она разбила один шаг на два — открытых
	// стало больше, закрытых столько же, судья на месте. Прежде это давало застой +1 и
	// правило роста, то есть предписанное лекарство приближало эскалацию.
	hist := []driftPoint{pp(3, 5, 3), pp(3, 5, 3), pp(3, 5, 3)} // застой 2
	cur := pp(3, 5, 4)
	if stall, _ := countStreaks(hist, cur); stall != 2 {
		t.Fatalf("уточнение плана засчитано застоем: ожидалось 2, получено %d", stall)
	}
	if _, reasons := evaluate(cur, d(1), d(4), hist, defaultLimits()); hasRule(reasons, "WORKER-DRIFT-03") {
		t.Error("новая строка плана названа регрессом: уточнение пути откатом не является")
	}
}

func TestВтороеУточнениеПодрядЗасчитываетсяЗастоем(t *testing.T) {
	// Бесплатно — один раз за цикл. Иначе описание пути можно наращивать вместо работы.
	hist := []driftPoint{pp(3, 5, 3), pp(3, 5, 4)} // первое уточнение — бесплатно
	if stall, _ := countStreaks(hist, pp(3, 5, 5)); stall != 1 {
		t.Fatalf("второе уточнение подряд без движения: ожидался застой 1, получено %d", stall)
	}
	// А после настоящего движения право на бесплатное уточнение возвращается.
	hist2 := []driftPoint{pp(3, 5, 3), pp(3, 5, 4), pp(3, 6, 3)}
	if stall, _ := countStreaks(hist2, pp(3, 6, 4)); stall != 0 {
		t.Fatalf("уточнение после закрытия шага: ожидался застой 0, получено %d", stall)
	}
}

func TestПереоткрытыйШагТормозит(t *testing.T) {
	v, reasons := evaluate(pp(1, 5, 23), d(1), d(23), []driftPoint{pp(1, 6, 22)}, defaultLimits())
	if v != verdictThrottle || !hasRule(reasons, "WORKER-DRIFT-03") {
		t.Fatalf("закрытый шаг снова открыт: ожидался %q с WORKER-DRIFT-03, получено %q %v", verdictThrottle, v, reasons)
	}
}

func TestСудьяДоволенАПланГоворитИное(t *testing.T) {
	// Пока план складывался в расстояние, этот случай давал ненулевое расстояние сам. Сумма
	// ушла — защита обязана остаться явным правилом.
	v, reasons := evaluate(pp(0, 3, 2), d(0), d(2), nil, defaultLimits())
	if v != verdictBlocked || !hasRule(reasons, "WORKER-DRIFT-04") {
		t.Fatalf("судья доволен при открытых шагах: ожидался %q с WORKER-DRIFT-04, получено %q", verdictBlocked, v)
	}
	if v2, _ := evaluate(pp(0, 5, 0), d(0), d(0), nil, defaultLimits()); v2 != verdictAllow {
		t.Fatalf("судья доволен и план закрыт: ожидался %q, получено %q", verdictAllow, v2)
	}
}

// --- граница истории и сквозной замер -------------------------------------------------

const planFixture = "| № | Шаг | Ступень | Судья |\n" +
	"|---|-----|---------|-------|\n" +
	"| ~~1~~ | сделано | `script` | ведущая |\n" +
	"| 2 | предстоит | `sonnet` | субагент |\n" +
	"| 3 | ещё | `sonnet` | субагент |\n" +
	"| 4 | выпуск | — | гейт: ЛПР |\n"

func fixtureProduct(t *testing.T, verdict, plan string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".goal-verdict.json"), []byte(verdict), 0o644); err != nil {
		t.Fatal(err)
	}
	if plan != "" {
		if err := os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte(plan), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestЗамерыСтарогоПравилаНеСравниваются(t *testing.T) {
	// Строки, записанные суммой, с новыми не сравниваются: у ASW остаток в них всё время 1,
	// и перечитанные по новому правилу они дали бы застой 13 на первом же замере.
	dir := t.TempDir()
	p := filepath.Join(dir, "goal-drift.jsonl")
	body := `{"distance":24,"judge_distance":1,"plan_open_steps":23}` + "\r\n" +
		`{"distance":24,"judge_distance":1,"plan_open_steps":23}` + "\r\n" +
		`{"distance":1,"distance_rule":"` + distanceRule + `","judge_distance":1,"plan_open_steps":22,"plan_closed_steps":3}` + "\r\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	h := readHistory(p)
	if len(h) != 1 {
		t.Fatalf("прочитано замеров %d, ожидался 1 — строки старого правила попали в счёт", len(h))
	}
	if h[0].Closed == nil || *h[0].Closed != 3 {
		t.Fatal("закрытые шаги из строки нового правила не прочитаны")
	}
}

func TestРасстояниеРавноОстаткуСудьи(t *testing.T) {
	dir := fixtureProduct(t, `{"code":1,"distance":1}`, planFixture)
	m, _, _ := measureDrift(dir, "")
	if m.Distance == nil || *m.Distance != 1 {
		t.Fatalf("расстояние обязано равняться остатку по судье 1, получено %v", m.Distance)
	}
	if m.DistanceRule != distanceRule {
		t.Errorf("замер не помечен правилом: %q", m.DistanceRule)
	}
	if m.PlanOpenSteps == nil || *m.PlanOpenSteps != 2 {
		t.Errorf("открытых исполняемых шагов ожидалось 2 (гейт не в счёт), получено %v", m.PlanOpenSteps)
	}
	if m.PlanClosedSteps == nil || *m.PlanClosedSteps != 1 {
		t.Errorf("закрытых шагов ожидался 1, получено %v", m.PlanClosedSteps)
	}
	if m.PlanGates != 1 {
		t.Errorf("гейтов ожидался 1, получено %d", m.PlanGates)
	}
}

func TestБезПланаРасстояниеИзмеримо(t *testing.T) {
	// Прежде отсутствие плана делало неизвестным всё расстояние. Остаток по судье от плана
	// не зависит, и называть его неизмеримым значило бы копить слепоту на пустом месте.
	dir := fixtureProduct(t, `{"code":1,"distance":2}`, "")
	m, _, limits := measureDrift(dir, "")
	if m.Distance == nil || *m.Distance != 2 {
		t.Fatalf("без плана расстояние обязано остаться остатком по судье 2, получено %v", m.Distance)
	}
	if m.PlanOpenSteps != nil {
		t.Error("без плана число открытых шагов обязано быть неизвестным, а не нулём")
	}
	if len(limits) == 0 {
		t.Error("отсутствие плана обязано называться в ограничениях замера")
	}
}

func TestЗаписанныйЗамерЧитаетсяОбратно(t *testing.T) {
	// ГЛАВНОЕ ИЗ ЭТИХ ПРОВЕРОК. Если запись забудет метку правила, чтение отбросит ВСЕ новые
	// строки, история навсегда останется пустой, застой никогда не вырастет — и двигатель
	// будет молча говорить ALLOW любой неподвижности. Отказ этого класса не кричит.
	dir := fixtureProduct(t, `{"code":1,"distance":1}`, planFixture)
	for i := 0; i < 3; i++ {
		m, _, _ := measureDrift(dir, "")
		recordMeasure(dir, m)
	}
	if h := readHistory(filepath.Join(dir, ".woody", "goal-drift.jsonl")); len(h) != 3 {
		t.Fatalf("записано 3 замера, прочитано обратно %d", len(h))
	}
	m, _, _ := measureDrift(dir, "")
	if m.StallMoves != 3 || m.Verdict != verdictThrottle {
		t.Fatalf("три неподвижных замера и текущий: ожидался застой 3 и %q, получено %d и %q",
			verdictThrottle, m.StallMoves, m.Verdict)
	}
}
