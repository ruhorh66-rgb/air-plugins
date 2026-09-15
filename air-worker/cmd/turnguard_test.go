package main

import "testing"

// TestCriterion20Pending — К20: ход, ждущий ЛПР, называет открытый гейт плана; без такого
// гейта ход не принимается.
func TestCriterion20Pending(t *testing.T) {
	// (a) ждёт ЛПР, открытых гейтов нет вовсе — отклонён.
	d := decideTurn(turnGuardInput{WaitLine: "жду решения ЛПР по гейту 3", OpenGates: nil})
	if d.Accept {
		t.Fatalf("ожидание ЛПР без открытых гейтов плана обязано отклонить ход: %+v", d)
	}

	// (b) ждёт ЛПР, гейты открыты, но названный номер не среди них — отклонён.
	d = decideTurn(turnGuardInput{WaitLine: "жду решения ЛПР по гейту 9", OpenGates: []string{"3", "5"}})
	if d.Accept {
		t.Fatalf("ход, называющий гейт вне списка открытых, обязан быть отклонён: %+v", d)
	}

	// (c) ждёт ЛПР и называет открытый гейт плана — принят.
	d = decideTurn(turnGuardInput{WaitLine: "жду решения ЛПР по гейту 3", OpenGates: []string{"3", "5"}})
	if !d.Accept {
		t.Fatalf("ход, называющий открытый гейт плана, обязан быть принят: %+v", d)
	}

	// (d) ход не заявляет ожидания ЛПР вовсе — правило К20 его не касается.
	d = decideTurn(turnGuardInput{WaitLine: "", OpenGates: nil})
	if !d.Accept {
		t.Fatalf("ход без заявленного ожидания ЛПР К20 не отклоняет: %+v", d)
	}

	// (e) продукт объявлен, план негоден: ход отклоняется кодом 2, даже если формально
	// названный гейт совпал бы, — план не годен раньше, чем считается ожидание ЛПР.
	d = decideTurn(turnGuardInput{
		WaitLine: "жду решения ЛПР по гейту 3", OpenGates: []string{"3"},
		ProductDeclared: true, PlanInvalid: true,
	})
	if d.Accept {
		t.Fatalf("объявленный продукт с негодным планом обязан отклонить ход (код 2): %+v", d)
	}
}

// TestCriterion22Pending — К22: режим оркестрации включён по умолчанию у сессии с
// объявленным продуктом; при включённой оркестрации ход, в котором код продукта изменила
// ведущая без субагента и без итерации петли, не принимается; правка плана ведущей разрешена.
func TestCriterion22Pending(t *testing.T) {
	// (a) оркестрация включена, ведущая одна изменила код продукта — отклонён.
	d := decideTurn(turnGuardInput{
		OrchestrationEnabled: true, LeaderChangedProduct: true,
	})
	if d.Accept {
		t.Fatalf("при включённой оркестрации правку кода продукта одной ведущей обязано отклонить: %+v", d)
	}

	// (b) тот же ход, но использован субагент — принят.
	d = decideTurn(turnGuardInput{
		OrchestrationEnabled: true, LeaderChangedProduct: true, SubagentUsed: true,
	})
	if !d.Accept {
		t.Fatalf("правку продукта с участием субагента обязано принять: %+v", d)
	}

	// (c) тот же ход, но идёт итерация петли — принят.
	d = decideTurn(turnGuardInput{
		OrchestrationEnabled: true, LeaderChangedProduct: true, LoopIterationRunning: true,
	})
	if !d.Accept {
		t.Fatalf("правку продукта при идущей итерации петли обязано принять: %+v", d)
	}

	// (d) ведущая правит только план — разрешено само по себе, даже без субагента и петли.
	d = decideTurn(turnGuardInput{
		OrchestrationEnabled: true, LeaderChangedProduct: false, LeaderEditedPlanOnly: true,
	})
	if !d.Accept {
		t.Fatalf("правка плана ведущей обязана быть разрешена при включённой оркестрации: %+v", d)
	}

	// (e) оркестрация выключена — правило К22 ход не отклоняет.
	d = decideTurn(turnGuardInput{
		OrchestrationEnabled: false, LeaderChangedProduct: true,
	})
	if !d.Accept {
		t.Fatalf("при выключенной оркестрации К22 ход не отклоняет: %+v", d)
	}

	// (f) продукт объявлен, но план негоден: даже субагент не спасает ход — план не годен
	// раньше, чем считается оркестрация.
	d = decideTurn(turnGuardInput{
		OrchestrationEnabled: true, LeaderChangedProduct: true, SubagentUsed: true,
		ProductDeclared: true, PlanInvalid: true,
	})
	if d.Accept {
		t.Fatalf("объявленный продукт с негодным планом обязан отклонить ход даже с субагентом: %+v", d)
	}
}

// TestCriterion34Pending — К34: вердикт двигателя действует и в сессии: при ESCALATE ход
// сессии не принимается так же, как останавливается петля, — пока причина не вынесена гейтом
// в план.
func TestCriterion34Pending(t *testing.T) {
	// (a) ESCALATE, причина не вынесена гейтом — отклонён.
	d := decideTurn(turnGuardInput{EngineVerdict: verdictEscalate, EscalationGated: false})
	if d.Accept {
		t.Fatalf("ESCALATE без гейта в плане обязан остановить ход сессии: %+v", d)
	}

	// (b) ESCALATE, причина вынесена гейтом плана — принят.
	d = decideTurn(turnGuardInput{EngineVerdict: verdictEscalate, EscalationGated: true})
	if !d.Accept {
		t.Fatalf("ESCALATE с причиной, вынесенной гейтом плана, обязан пропускать ход: %+v", d)
	}

	// (c) вердикт не ESCALATE (ALLOW/THROTTLE/ЖДЁТ ЛПР) — К34 ход не отклоняет сам по себе.
	for _, v := range []string{verdictAllow, verdictThrottle, verdictBlocked, ""} {
		d = decideTurn(turnGuardInput{EngineVerdict: v, EscalationGated: false})
		if !d.Accept {
			t.Fatalf("вердикт %q не ESCALATE, К34 не обязан отклонять ход: %+v", v, d)
		}
	}

	// (d) продукт объявлен, план негоден: ход отклоняется кодом 2 даже при ALLOW и без
	// эскалации — негодный план останавливает ход раньше, чем вердикт двигателя.
	d = decideTurn(turnGuardInput{
		EngineVerdict: verdictAllow, ProductDeclared: true, PlanInvalid: true,
	})
	if d.Accept {
		t.Fatalf("объявленный продукт с негодным планом обязан отклонить ход независимо от вердикта двигателя: %+v", d)
	}
}
