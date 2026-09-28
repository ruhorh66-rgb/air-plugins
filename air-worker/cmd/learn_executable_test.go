package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withExecutableCheck(t *testing.T, check executableLearnCheck) {
	t.Helper()
	key := executableLearnKey(check.CheckType, check.CheckSpec)
	old, had := executableLearnChecks[key]
	executableLearnChecks[key] = check
	t.Cleanup(func() {
		if had {
			executableLearnChecks[key] = old
		} else {
			delete(executableLearnChecks, key)
		}
	})
}

func executableProposal(id, checkType, checkSpec string) learnProposal {
	return learnProposal{
		Schema: learnSchemaVersion, ID: id, CreatedAt: "2026-09-28T12:00:00Z",
		Status: learnPending, Class: "arbitrary-learned-class", Rule: "enforce a verified learned rule",
		Trigger: "test trigger", CheckType: checkType, CheckSpec: checkSpec,
		TestCase: "fixture violation -> expected block",
	}
}

func TestExecutableRegistryRemovesClassHardcodeFromSGT(t *testing.T) {
	p := executableProposal("LP-registry-sgt", "hook", learnCheckSGTCommitFreshness)
	if err := supportsExecutableLearnProposal(p); err != nil {
		t.Fatalf("registered SGT check rejected for an unrelated class: %v", err)
	}
	rule, err := verifiedExecutableRule(p, learnApprovalGrant{ID: "LG-test"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rule.Class != p.Class || !strings.Contains(rule.VerificationReceipt, "cases=4/4") {
		t.Fatalf("generic compilation lost proposal/receipt: %#v", rule)
	}
}

func TestExecutableRegistryCompilesHookGateAndScriptTransactions(t *testing.T) {
	for _, checkType := range []string{"hook", "gate", "script"} {
		checkType := checkType
		t.Run(checkType, func(t *testing.T) {
			product := t.TempDir()
			spec := "fixture-" + checkType + "-v1"
			withExecutableCheck(t, executableLearnCheck{
				CheckType: checkType, CheckSpec: spec,
				Verify: func(p learnProposal) (string, error) {
					return "fixture:" + p.CheckType + ":verified", nil
				},
			})
			p := executableProposal("LP-"+checkType, checkType, spec)
			if err := appendLearnJSON(learnPaths(product).Proposals, p); err != nil {
				t.Fatal(err)
			}
			grantLearnProposal(t, product, p.ID)
			if code := cmdLearnApply([]string{"-product", product, "-id", p.ID}); code != 0 {
				t.Fatalf("generic %s apply code=%d", checkType, code)
			}
			raw, err := os.ReadFile(executableRulePath(learnPaths(product), p.ID))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"check_type": "`+checkType+`"`) ||
				!strings.Contains(string(raw), "fixture:"+checkType+":verified") {
				t.Fatalf("compiled %s manifest missing generic receipt: %s", checkType, raw)
			}
		})
	}
}

func TestExecutableHookDispatchUsesRegistryAndFailsClosedOnMissingImplementation(t *testing.T) {
	product := t.TempDir()
	spec := "fixture-hook-dispatch-v1"
	called := false
	withExecutableCheck(t, executableLearnCheck{
		CheckType: "hook", CheckSpec: spec,
		Verify: func(learnProposal) (string, error) { return "fixture:verified", nil },
		PreTool: func(_ string, rule learnRuleRecord, command string, _ time.Time) (bool, string, error) {
			called = true
			return strings.Contains(command, "forbidden"), rule.ProposalID + ": forbidden", nil
		},
	})
	rule := learnRuleRecord{
		Schema: learnRuleSchemaVersion, ProposalID: "LP-dispatch", Class: "scope",
		Rule: "block forbidden", Trigger: "before tool", CheckType: "hook", CheckSpec: spec,
		TestCase: "forbidden -> block", Status: learnApplied,
		ApprovedAt: "2026-09-28T12:00:00Z", Approval: "grant:LG-x",
		VerifiedAt: "2026-09-28T12:00:00Z", VerificationReceipt: "fixture:verified",
	}
	path := executableRulePath(learnPaths(product), rule.ProposalID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := jsonMarshalIndent(rule)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	blocked, reason, err := enforceExecutableLearnHooks(product, "do forbidden thing", time.Now())
	if err != nil || !blocked || !called || !strings.Contains(reason, "LP-dispatch") {
		t.Fatalf("registry hook dispatch blocked=%v called=%v reason=%q err=%v", blocked, called, reason, err)
	}

	delete(executableLearnChecks, executableLearnKey("hook", spec))
	if _, _, err := enforceExecutableLearnHooks(product, "do forbidden thing", time.Now()); err == nil {
		t.Fatal("active learned hook without registered implementation did not fail closed")
	}
}

func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
