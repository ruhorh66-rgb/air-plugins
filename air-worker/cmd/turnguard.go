package main

import "regexp"

// СТРАЖ ХОДА — ЧИСТАЯ ФУНКЦИЯ. Решает, принимается ли ход (сессии или петли) по фактам,
// переданным вызывающим кодом: сама она диск не читает и хуки не подключает — подключение
// к событию хоста остаётся отдельным шагом (по образцу `classifyBypass`, cmd/bypassguard.go).
//
// ЛПР 14.09.2026, шаг 41: страж «просто сверяет два числа» — сверка без действия по вердикту
// ухода в сторону не останавливает. Три решения этого файла (К20, К22, К34) и есть то
// действие: не только назвать расхождение, но и отклонить ход, пока причина не закрыта планом.

// reGateRef — ссылка на гейт плана в тексте ожидания ЛПР: слово «гейт» и номер шага. Тот же
// разбор, что в turn-guard.ps1 (rule 4), перенесён в бинарник буквально.
var reGateRef = regexp.MustCompile(`(?i)гейт[а-я]*\s*0*([0-9]+[A-Za-zА-Яа-я]?)`)

// turnGuardInput — факты одного хода. Ничего не выводится из файловой системы внутри
// decideTurn: значения обязан собрать вызывающий код (goals.go для OpenGates, drift.go для
// Drift, goals.go/lockHeld для LoopIterationRunning).
type turnGuardInput struct {
	// WaitLine — дословная строка хода «От тебя жду: ...». Пустая строка значит «ход не
	// заявляет ожидания ЛПР вовсе» — К20 такой ход не трогает.
	WaitLine string
	// OpenGates — номера открытых гейтов плана (та же форма, что goals.go печатает в
	// open_gates).
	OpenGates []string

	// OrchestrationEnabled — режим оркестрации включён для сессии (К22: по умолчанию
	// включён, если продукт объявлен).
	OrchestrationEnabled bool
	// LeaderChangedProduct — этим ходом ведущая сама (без субагента) изменила код продукта.
	LeaderChangedProduct bool
	// LeaderEditedPlanOnly — изменения хода ограничены файлом плана; правка плана ведущей
	// разрешена всегда, даже при включённой оркестрации.
	LeaderEditedPlanOnly bool
	// SubagentUsed — ход опирается на результат субагента (Task/Agent tool).
	SubagentUsed bool
	// LoopIterationRunning — по продукту идёт итерация петли (тот же признак, что
	// goals.go считает через lockHeld(lockName("loop", root))).
	LoopIterationRunning bool

	// EngineVerdict — последний вердикт двигателя цели (verdictAllow/Throttle/Escalate/
	// Blocked из drift.go). К34: ESCALATE останавливает ход сессии так же, как петлю.
	EngineVerdict string
	// EscalationGated — причина эскалации вынесена гейтом в план (открытый гейт плана
	// существует и назван для этой эскалации). Без этого ESCALATE ход не пропускает.
	EscalationGated bool

	// ProductDeclared — сессия объявила продукт (cmd/session.go, cmdSessionDeclare).
	ProductDeclared bool
	// PlanInvalid — план объявленного продукта негоден (productReady/goals вернули
	// проблемы). Продукт объявлен, а план не годен — с 0.10.1 это код 2 у судьи, двигателя
	// и отчёта; страж хода отвечает тем же кодом, а не молчит.
	PlanInvalid bool
}

// turnGuardDecision — итог. Reason пуст только когда Accept истинен.
type turnGuardDecision struct {
	Accept bool
	Reason string
}

// decideTurn — единственная точка решения стража хода. Порядок правил значения не имеет:
// каждое смотрит на непересекающийся набор полей входа, и совпасть могут независимо.
func decideTurn(in turnGuardInput) turnGuardDecision {
	// Продукт объявлен, а его план не годен — с 0.10.1 судья, двигатель и отчёт отвечают
	// на этом кодом 2; страж хода называет это в ходе, а не пропускает ход молча.
	if in.ProductDeclared && in.PlanInvalid {
		return turnGuardDecision{Accept: false,
			Reason: "продукт объявлен, план негоден (код 2) — ход не принимается, пока план не годен"}
	}

	// К34: вердикт двигателя ESCALATE действует и в сессии — ход не принимается, пока
	// причина не вынесена гейтом в план (то же самое правило, что останавливает петлю,
	// cmd/loop.go, closeWoody при driftCode==2).
	if in.EngineVerdict == verdictEscalate && !in.EscalationGated {
		return turnGuardDecision{Accept: false,
			Reason: "двигатель вынес ESCALATE, а причина не названа гейтом в плане — ход не принимается, как останавливается петля"}
	}

	// К22: оркестрация включена по умолчанию у сессии с объявленным продуктом; правка
	// плана ведущей разрешена сама по себе (даже без субагента и петли).
	if in.OrchestrationEnabled && in.LeaderChangedProduct && !in.LeaderEditedPlanOnly &&
		!in.SubagentUsed && !in.LoopIterationRunning {
		return turnGuardDecision{Accept: false,
			Reason: "оркестрация включена: код продукта изменила ведущая без субагента и без итерации петли — такой ход не принимается"}
	}

	// К20: ход, ждущий ЛПР, обязан назвать открытый гейт плана; без такого гейта — не
	// принимается. Ход, не заявляющий ожидания (WaitLine пуст), это правило не касается.
	if in.WaitLine != "" && !waitNamesOpenGate(in.WaitLine, in.OpenGates) {
		if len(in.OpenGates) == 0 {
			return turnGuardDecision{Accept: false,
				Reason: "ход ждёт ЛПР, но в плане нет ни одного открытого гейта — ждать нечего, ход не принимается"}
		}
		return turnGuardDecision{Accept: false,
			Reason: "ход ждёт ЛПР, но не называет ни одного из открытых гейтов плана — ход не принимается"}
	}

	return turnGuardDecision{Accept: true}
}

// waitNamesOpenGate — строка ожидания называет один из переданных открытых гейтов по номеру.
func waitNamesOpenGate(wait string, openGates []string) bool {
	m := reGateRef.FindAllStringSubmatch(wait, -1)
	if len(m) == 0 {
		return false
	}
	named := map[string]bool{}
	for _, g := range m {
		named[g[1]] = true
	}
	for _, g := range openGates {
		if named[g] {
			return true
		}
	}
	return false
}
