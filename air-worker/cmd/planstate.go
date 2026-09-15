package main

// PlanState — ЕДИНЫЙ ВЗГЛЯД НА ПЛАН (этап 0.10.6, К40).
//
// До этого шага goals/report/drift/loop каждый по-своему решал, что план плана: report и
// drift читали остаток по судье и шаги плана раздельно, goals пересчитывал открытые гейты
// вручную, а petля свой счёт «гейтов/исполняемых» — четыре места, которые обязаны
// СОГЛАСИТЬСЯ и ничем, кроме памяти автора, друг с другом не связаны. buildPlanState —
// одна функция, которую зовут все четыре: расхождение между ними становится невозможным,
// а не маловероятным (тот же довод, что был у parsePlan против readPlanSteps).
//
// GATED здесь отделён от UNKNOWN (см. measureGated в criteria.go): LPRGates — это открытые
// гейты ПЛАНА плюс критерии, чья мера объявлена решением ЛПР, а не «непонятно, чем мерить».
// Закрытые гейты в LPRGates не входят — закрытый гейт не работа и не ожидание, он сделан.
type PlanState struct {
	Path  string
	Steps []planStep
	Goals planGoals

	CriteriaPassed  []string
	CriteriaFailed  []string
	CriteriaGated   []string
	CriteriaUnknown []string

	// PlanGates — открытые гейты САМОГО ПЛАНА (шаги, закрываемые человеком).
	PlanGates int
	// ExecutableWork — сколько шагов плана сессия ещё может закрыть работой (без гейтов).
	ExecutableWork int
	// LPRGates — ВСЕ гейты ЛПР: гейты плана + критерии, ждущие решения. Отдельная величина
	// от ExecutableWork и от TechnicalUnknowns — работой она не лечится.
	LPRGates int
	// TechnicalUnknowns — критериев, которым нечем измериться (не путать с LPRGates).
	TechnicalUnknowns int
}

// buildPlanState — читает план и критерии ОДИН РАЗ и по одному правилу.
//
// base — результат уже прогнанных проверок судьи (judgeResult). Вызовы, которые проверки
// не гоняют (goals, report при идущей петле, drift), передают пустой judgeResult{}:
// критерии с мерой «проверка» тогда честно уходят в unknown («проверка не дала машинного
// результата») — то же самое поведение, что у checkResultState с пустой базой, ничего
// нового этот путь не решает молча.
func buildPlanState(root string, cfg runConfig, planPath string, base judgeResult) PlanState {
	plan := parsePlan(planPath)
	g := readPlanGoals(planPath)
	ps := PlanState{
		Path:           planPath,
		Steps:          plan.Steps,
		Goals:          g,
		PlanGates:      plan.Gates(),
		ExecutableWork: plan.OpenWork(),
	}
	if len(g.Criteria) > 0 {
		ps.CriteriaPassed, ps.CriteriaFailed, ps.CriteriaGated, ps.CriteriaUnknown = evaluatePlanCriteria(root, cfg, g, base)
	}
	ps.LPRGates = ps.PlanGates + len(ps.CriteriaGated)
	ps.TechnicalUnknowns = len(ps.CriteriaUnknown)
	return ps
}
