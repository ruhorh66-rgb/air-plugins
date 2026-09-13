package main

import "testing"

// Тесты проверяют ПРАВИЛА, а не прогон на живом продукте. Прогон доказывает, что сегодня
// на этой машине получилось; тест доказывает, что правило такое, каким объявлено.

func d(v int) *int { return &v }

func histOf(vals ...any) []*int {
	out := make([]*int, 0, len(vals))
	for _, v := range vals {
		if v == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, d(v.(int)))
	}
	return out
}

func TestЛестницаЗастоя(t *testing.T) {
	lim := defaultLimits()
	// Расстояние 4 не двигается. Порог торможения 3, эскалации 6.
	cases := []struct {
		history []*int
		want    string
	}{
		{histOf(), verdictAllow},                       // первый замер: сравнивать не с чем
		{histOf(4), verdictAllow},                      // застой 1
		{histOf(4, 4), verdictAllow},                   // застой 2
		{histOf(4, 4, 4), verdictThrottle},             // застой 3 — порог торможения
		{histOf(4, 4, 4, 4, 4), verdictThrottle},       // застой 5
		{histOf(4, 4, 4, 4, 4, 4), verdictEscalate},    // застой 6 — порог эскалации
	}
	for i, c := range cases {
		got, _ := evaluate(d(4), d(1), c.history, lim)
		if got != c.want {
			t.Errorf("случай %d: история %d замеров -> %q, ожидалось %q", i, len(c.history), got, c.want)
		}
	}
}

func TestДвижениеСнимаетТормозСразу(t *testing.T) {
	// Пять замеров без движения — и шестой с уменьшением. Тормоз обязан сняться тем же
	// ходом, а не «отстояться»: иначе механизм наказывал бы за уже исправленное.
	got, _ := evaluate(d(3), d(1), histOf(5, 5, 5, 5, 5), defaultLimits())
	if got != verdictAllow {
		t.Fatalf("после уменьшения расстояния ожидался %q, получено %q", verdictAllow, got)
	}
}

func TestНеизмеримоеНеРавноЧистому(t *testing.T) {
	// Судья с кодом 2 даёт расстояние nil. Два таких подряд — эскалация, а не «чисто».
	if got, _ := evaluate(nil, d(2), histOf(), defaultLimits()); got != verdictAllow {
		t.Errorf("один слепой замер: ожидался %q, получено %q", verdictAllow, got)
	}
	if got, _ := evaluate(nil, d(2), histOf(nil), defaultLimits()); got != verdictEscalate {
		t.Errorf("два слепых замера подряд: ожидался %q, получено %q", verdictEscalate, got)
	}
}

func TestНулевоеРасстояниеНеЗастаивается(t *testing.T) {
	// Расстояние ноль не уменьшается НИКОГДА. Без этого правила продукт с исчерпанной
	// работой получал бы эскалацию за то, что работа кончилась. Найдено AIR-ENV-002.
	long := histOf(0, 0, 0, 0, 0, 0, 0, 0)
	got, _ := evaluate(d(0), d(1), long, defaultLimits())
	if got != verdictBlocked {
		t.Fatalf("восемь замеров при нуле: ожидался %q, получено %q", verdictBlocked, got)
	}
	got, _ = evaluate(d(0), d(0), long, defaultLimits())
	if got != verdictAllow {
		t.Fatalf("ноль при согласном судье: ожидался %q, получено %q", verdictAllow, got)
	}
}

func TestРостРасстоянияТормозитОтдельно(t *testing.T) {
	// Застой — это ноль движения; рост означает, что сделанное разломало уже закрытое.
	v, reasons := evaluate(d(5), d(1), histOf(3), defaultLimits())
	if v != verdictThrottle {
		t.Fatalf("рост расстояния: ожидался %q, получено %q", verdictThrottle, v)
	}
	found := false
	for _, r := range reasons {
		if r.Rule == "WORKER-DRIFT-03" {
			found = true
		}
	}
	if !found {
		t.Error("рост расстояния должен называться правилом WORKER-DRIFT-03 отдельно от застоя")
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
