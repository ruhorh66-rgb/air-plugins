package main

import "strings"

// Ступень и исполнитель — РАЗНЫЕ ВЕЩИ. Ступень говорит, сколько мы готовы заплатить;
// исполнитель — чем именно исполняем. Одна лестница может смешивать вендоров:
// script -> haiku -> codex -> opus, если так решено для продукта.

type runnerSpec struct {
	Kind   string `json:"kind"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type ladderMatch struct {
	Found bool
	Index int
	Kind  string // exact | model-prefix | none
}

// resolveLadderTier — сперва точное совпадение, при промахе по имени модели до
// двоеточия, при промахе обоих — отказ с названной причиной.
//
// НЕ МОЛЧАЛИВЫЙ ИНДЕКС 0. Прежде план говорил `sonnet`, лестница держала только
// `sonnet:medium` и `sonnet:max`, совпадения не было — и код МОЛЧА подставлял нулевую
// ступень: объявленная ступень тихо подменялась самой низкой, и петля карабкалась с нуля
// незаметно для плана.
func resolveLadderTier(ladder []string, tier string) ladderMatch {
	if tier == "" {
		return ladderMatch{Kind: "none", Index: -1}
	}
	if len(ladder) == 0 {
		return ladderMatch{Kind: "no-ladder", Index: -1}
	}
	for i, r := range ladder {
		if r == tier {
			return ladderMatch{Found: true, Index: i, Kind: "exact"}
		}
	}
	name := tierName(tier)
	for i, r := range ladder {
		if tierName(r) == name {
			return ladderMatch{Found: true, Index: i, Kind: "model-prefix"}
		}
	}
	return ladderMatch{Kind: "none", Index: -1}
}

// tierName — имя модели до двоеточия. Ступень записывается тройкой «вендор · модель ·
// усилие» одной строкой вида «sonnet:high»; второй оси нет намеренно, чтобы лестница
// оставалась плоским списком, который человек правит руками и видит глазами.
func tierName(rung string) string {
	if i := strings.Index(rung, ":"); i > 0 {
		return rung[:i]
	}
	return rung
}

func tierEffort(rung string) string {
	if i := strings.Index(rung, ":"); i > 0 {
		return strings.TrimSpace(rung[i+1:])
	}
	return ""
}

// resolveRunner — чем исполняется ступень.
//
// Умолчание: имя ступени и есть имя модели Claude. Так лестница работает без раздела
// runners вовсе — он нужен только чтобы подмешать другого вендора.
func resolveRunner(cfg runConfig, rung string) runnerSpec {
	name := tierName(rung)
	effort := tierEffort(rung)

	if r, ok := cfg.Runners[name]; ok {
		out := runnerSpec{Kind: r.Kind, Model: r.Model, Effort: effort}
		if out.Kind == "" {
			out.Kind = "claude"
		}
		if out.Model == "" {
			out.Model = name
		}
		if out.Effort == "" {
			out.Effort = r.Effort
		}
		return out
	}
	if name == "script" {
		return runnerSpec{Kind: "script"}
	}
	return runnerSpec{Kind: "claude", Model: name, Effort: effort}
}
