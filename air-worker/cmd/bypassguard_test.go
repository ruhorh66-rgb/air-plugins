package main

import "testing"

// СТРАЖ ОБХОДА проверяется НАБОРОМ КОМАНД: решение принимается по тексту (и по путям
// установки, параметру), значит и проверяется текстом. Каждая строка ниже — либо живой
// обход, который обязан быть пойман, либо законная команда, которую нельзя принять за
// обход.

// testInstallDir — каталог установки air-worker для правила 2: копия ПОВЕРХ него — обход,
// копия куда-то ещё (черновой каталог, сборка в bin/ рабочего дерева) — нет.
const testInstallDir = `C:\Users\u\AppData\Local\air-worker`

func TestСтражОбхода(t *testing.T) {
	dirs := []string{testInstallDir}
	bypass := []string{
		// 1) подмена маркетплейса локальным путём вместо GitHub owner/repo
		`claude plugin marketplace add F:\-8-\air-plugins`,
		`claude plugin marketplace add C:\x\air-plugins`,
		`claude plugin marketplace add ./air-plugins`,
		`claude plugin marketplace add ../air-plugins`,
		`claude plugin marketplace add /home/u/air-plugins`,
		// 2) копирование/перемещение в каталог установки или в кэш плагина (правило
		// сужено: раньше ловило любую команду с "air-worker.exe" в строке)
		`copy build\air-worker.exe "C:\Users\u\AppData\Local\air-worker\bin\air-worker.exe"`,
		`Copy-Item .\air-worker.exe C:\Users\u\AppData\Local\air-worker\bin\air-worker.exe`,
		`move air-worker.exe C:\Users\u\AppData\Local\air-worker\bin\air-worker.exe`,
		`copy build\air-worker.exe "C:\Users\u\.claude\plugins\cache\air-plugins\air-worker\0.10.1\bin\air-worker.exe"`,
		// 3) запуск air-worker.exe install не из кэша плагина
		`F:\-7-\air-worker\bin\air-worker.exe install`,
		`"F:\-7-\air-worker\bin\air-worker.exe" install -autostart`,
	}
	for _, c := range bypass {
		if ok, why := classifyBypass(c, dirs); !ok || why == "" {
			t.Errorf("обход не пойман: %q (ok=%v, why=%q)", c, ok, why)
		}
	}

	allowed := []string{
		`claude plugin marketplace add ruhorh66-rgb/air-plugins`,
		`claude plugin marketplace add https://github.com/ruhorh66-rgb/air-plugins`,
		`E:\-4-\claude-home\plugins\cache\air-plugins\air-worker\0.10.1\bin\air-worker.exe install`,
		`air-worker install`, // голое имя: путь неизвестен, обходом не объявляем
		`claude plugin update air-worker@air-plugins`,
		`air-worker judge -product .`,
		// копия бинарника из рабочего дерева в черновой каталог — не обход: ни
		// каталога установки, ни кэша плагина в пути нет.
		`copy F:\-7-\air-worker\bin\air-worker.exe F:\-7-\_worktrees\aw-step66\scratch\air-worker.exe`,
		// сборка выпуска в bin/ — не команда копирования/перемещения вовсе.
		`go build -o bin/air-worker.exe`,
		`git status`,
		``,
	}
	for _, c := range allowed {
		if ok, why := classifyBypass(c, dirs); ok {
			t.Errorf("законная команда принята за обход: %q (why=%q)", c, why)
		}
	}
}

// TestСтражОбходаПустыеКаталоги — без installDirs (nil) правило 2 не разваливается и не
// паникует: копия в каталог установки такой вызов уже не ловит (путей нет), но копия в
// кэш плагина по-прежнему ловится, потому что это отдельное условие.
func TestСтражОбходаПустыеКаталоги(t *testing.T) {
	cache := `copy build\air-worker.exe "C:\Users\u\.claude\plugins\cache\air-plugins\air-worker\0.10.1\bin\air-worker.exe"`
	if ok, why := classifyBypass(cache, nil); !ok || why == "" {
		t.Errorf("копия в кэш плагина обязана ловиться даже без installDirs: ok=%v why=%q", ok, why)
	}
	toInstallDir := `copy build\air-worker.exe "` + testInstallDir + `\bin\air-worker.exe"`
	if ok, _ := classifyBypass(toInstallDir, nil); ok {
		t.Error("без installDirs копию в каталог установки ловить нечем — это ожидаемо, не паника")
	}
}

func TestN130CompoundAuthorizedMarketplaceCannotMaskLaterBypass(t *testing.T) {
	examples := []struct {
		name    string
		cmd     string
		blocked bool
	}{
		{"valid_single", "claude plugin marketplace add ruhorh66-rgb/air-plugins", false},
		{"market_then_codex_semicolon", "claude plugin marketplace add ruhorh66-rgb/air-plugins; codex exec --json", true},
		{"market_then_codex_and", "claude plugin marketplace add ruhorh66-rgb/air-plugins && codex exec --json", true},
		{"market_then_codex_newline", "claude plugin marketplace add ruhorh66-rgb/air-plugins\ncodex app-server --listen stdio://", true},
		{"market_then_claude_print", "claude plugin marketplace add ruhorh66-rgb/air-plugins | claude -p 'review'", true},
		{"market_then_local_marketplace", "claude plugin marketplace add ruhorh66-rgb/air-plugins; claude plugin marketplace add F:\\evil\\plugins", true},
		{"market_then_install_copy", "claude plugin marketplace add ruhorh66-rgb/air-plugins; Copy-Item .\\x.exe C:\\Users\\u\\.claude\\plugins\\cache\\air-plugins\\air-worker\\x.exe", true},
		{"valid_market_then_native", "claude plugin marketplace add ruhorh66-rgb/air-plugins; air-worker plan-review -product . -executor codex", false},
	}
	for _, tc := range examples {
		t.Run(tc.name, func(t *testing.T) {
			denied, reason := classifyBypass(tc.cmd, []string{testInstallDir})
			if denied != tc.blocked {
				t.Fatalf("blocked=%v want=%v reason=%q command=%q", denied, tc.blocked, reason, tc.cmd)
			}
			if tc.blocked && reason == "" {
				t.Fatal("blocked command must provide a concrete safe native-route reason")
			}
		})
	}
}

func TestN130AnthropicP1FlagFirstMarketplaceAndCodexGlobalFlags(t *testing.T) {
	for _, command := range []string{
		`claude plugin marketplace add --scope user F:\\-8-\\air-plugins`,
		`claude plugin marketplace add --scope=user .`,
		`claude plugin marketplace add .`,
		`claude plugin marketplace add mydir`,
		`claude plugin marketplace add x/..`,
		`claude plugin marketplace add https://example.org/fake/plugin`,
		`codex -c model_reasoning_effort=low exec --json`,
		`codex --model gpt-6-sol e some-task`,
		`codex --model gpt-6-sol app-server`,
		`& "C:\\Tools\\codex.cmd" --model gpt-6-sol exec --json`,
	} {
		if denied, reason := classifyBypass(command, nil); !denied || reason == "" {
			t.Errorf("reviewer P1 command was allowed: %q (%s)", command, reason)
		}
	}
	for _, command := range []string{
		`claude plugin marketplace add ruhorh66-rgb/air-plugins`,
		`claude plugin marketplace add --scope user ruhorh66-rgb/air-plugins`,
		`claude plugin marketplace add https://github.com/ruhorh66-rgb/air-plugins`,
		`codex login status`,
		`codex --version`,
		`git grep -n 'codex exec' -- cmd`,
		`Write-Output 'claude auth status'`,
	} {
		if denied, reason := classifyBypass(command, nil); denied {
			t.Errorf("allowed diagnostic was blocked: %q (%s)", command, reason)
		}
	}
}

func TestN130AnthropicShellWrappersMustNotHideVendorModelCalls(t *testing.T) {
	denied := []string{
		`cmd /c codex exec --json -m gpt-6-sol`,
		`cmd.exe /d /c "codex --model gpt-6-sol exec --json"`,
		`powershell -NoProfile -Command "codex exec --json"`,
		`pwsh -NonInteractive -c '& "C:\Tools\codex.cmd" exec --json'`,
		`bash -c 'claude -p "review"'`,
		`sh -c "codex e task"`,
		`env X=1 codex exec --json`,
		`env FOO=bar claude --print prompt`,
		`npx @openai/codex exec --json`,
		`npx -y @openai/codex e hello`,
		`call codex exec --json`,
		`start codex exec --json`,
		`(codex exec --json)`,
		`$(codex exec --json)`,
		`"codex" exec --json`,
		`'claude' -p hi`,
		"git status; cmd /c codex exec --json",
		"Write-Output safe; powershell -Command 'claude -p hi'",
		"claude plugin marketplace add ruhorh66-rgb/air-plugins && bash -c 'codex exec --json'",
	}
	for _, command := range denied {
		if blocked, reason := classifyBypass(command, nil); !blocked || reason == "" {
			t.Errorf("wrapped vendor model bypass not blocked: %q (%s)", command, reason)
		}
	}
	allowed := []string{
		`codex login status`, `codex --version`, `claude --version`,
		`git grep -n 'codex exec' -- cmd`,
		`Write-Output 'claude auth status'`,
		`echo 'codex exec is forbidden here'`,
		`powershell -NoProfile -Command "Write-Output 'codex exec is forbidden'"`,
		`bash -c 'echo "claude -p is forbidden"'`,
		`air-worker review source -node N-130 -executor codex`,
		`air-worker orchestrate -product . -subagents 2`,
	}
	for _, command := range allowed {
		if blocked, reason := classifyBypass(command, nil); blocked {
			t.Errorf("benign diagnostic/documentation blocked: %q (%s)", command, reason)
		}
	}
}

func TestN130AdditionalAnthropicShellPrefixes(t *testing.T) {
	blocked := []string{
		`then codex exec --json`,
		`do claude --print "review"`,
		`{ codex exec --json; }`,
		`nohup codex exec --json`,
		`time claude -p prompt`,
		`xargs codex exec --json`,
		`xargs -n 1 codex e task`,
		`echo $(claude -p review)`,
		"echo " + string(rune(96)) + "codex exec --json" + string(rune(96)),
		`cmd /c "if exist a (codex exec --json)"`,
	}
	for _, s := range blocked {
		if deny, reason := classifyBypass(s, nil); !deny || reason == "" {
			t.Errorf("secondary wrapper/command substitution allowed: %q (%s)", s, reason)
		}
	}
	for _, s := range []string{
		`then echo safe`,
		`time git status`,
		`xargs echo hello`,
		`nohup air-worker report -cached`,
		`git grep -n "then codex exec" -- cmd`,
	} {
		if deny, reason := classifyBypass(s, nil); deny {
			t.Errorf("safe secondary wrapper blocked: %q (%s)", s, reason)
		}
	}
}

func TestN130AnthropicNativeWrapperNpxAndEnvOptions(t *testing.T) {
	denied := []string{
		"npx @anthropic-ai/claude-code -p review",
		"npx -y @anthropic-ai/claude-code --print review",
		"npx --yes @anthropic-ai/claude-code --print review",
		"env -u CLAUDE_CONFIG_DIR claude -p review",
		"env --unset CLAUDE_CONFIG_DIR codex exec --json",
		"env --unset=CLAUDE_CONFIG_DIR claude --print review",
		"env -C own-temp-dir codex exec --json",
		"env --chdir own-temp-dir claude -p review",
		"env --chdir=own-temp-dir codex app-server",
		"env -S 'claude -p review'",
		"env -S 'bash -c \"codex exec --json\"'",
		"env -- claude -p review",
		"env -i -u CLAUDE_CONFIG_DIR -C own-temp-dir -- codex e task",
		"env --ignore-environment --unset=CLAUDE_CONFIG_DIR --chdir=own-temp-dir claude -p review",
		"env -uCLAUDE_CONFIG_DIR claude --print review",
		"env -Cown-temp-dir codex app-server",
		"env --split-string 'claude --print review'",
		"env --split-string='codex exec --json'",
	}
	for _, command := range denied {
		if denied, why := classifyBypass(command, nil); !denied || why == "" {
			t.Errorf("model invocation was not denied: %q (%s)", command, why)
		}
	}
	allowed := []string{
		"npx @anthropic-ai/claude-code --version",
		"env -- codex --version",
		"env -u CLAUDE_CONFIG_DIR git status",
		"env -C own-temp-dir git status",
		"env --chdir=own-temp-dir air-worker version",
		"env -S 'git status'",
		"npx -y @anthropic-ai/claude-code --version",
		"npx --yes @anthropic-ai/claude-code --version",
		"env -i -u CLAUDE_CONFIG_DIR git status",
		"env --ignore-environment --chdir own-temp-dir -- codex --version",
		"env --split-string='git status'",
	}
	for _, command := range allowed {
		if denied, why := classifyBypass(command, nil); denied {
			t.Errorf("legitimate diagnostic was denied: %q (%s)", command, why)
		}
	}
}

func TestN130AnthropicP1PathQualifiedAndOpaqueShellLaunchers(t *testing.T) {
	denied := []string{
		`C:\Windows\System32\cmd.exe /c "codex exec --json"`,
		`"C:\Windows\System32\cmd.exe" /c "claude --print review"`,
		`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "codex exec --json"`,
		`"C:\Program Files\PowerShell\7\pwsh.exe" -NoProfile -Command "claude -p review"`,
		`/bin/bash -c 'claude -p review'`,
		`/usr/bin/bash -ec 'codex exec --json'`,
		`/bin/sh -xc 'claude --print review'`,
		`bash -ec 'claude -p review'`,
		`bash -xc 'codex e review'`,
		`bash --rcfile NUL -c 'claude -p review'`,
		`bash --init-file NUL -ec 'codex exec --json'`,
		`bash "-c" "claude -p hi"`,
		`sh '-ec' 'codex exec x'`,
		`bash "-o" pipefail "-c" "claude --print hi"`,
		`bash "--rcfile" NUL "-c" "claude -p hi"`,
		`claude "-p" "review"`,
		`claude '--print' review`,
		`codex "exec" --json`,
		`codex 'app-server'`,
		`codex "e" --json`,
		`"claude" "-p" "review"`,
		`& "claude" "-p" review`,
		`powershell -EncodedCommand YwBsAGEAdQBkAGUAIAAtAHA=`,
		`pwsh -EncodedCommand YwBsAGEAdQBkAGUAIAAtAHA=`,
	}
	for _, command := range denied {
		if blocked, reason := classifyBypass(command, nil); !blocked || reason == "" {
			t.Errorf("N130 path-qualified/opaque shell bypass escaped guard: %q (%s)", command, reason)
		}
	}
	allowed := []string{
		`C:\Windows\System32\cmd.exe /c "echo safe"`,
		`/bin/bash -ec 'git status'`,
		`/usr/bin/sh -c 'echo hello'`,
		`bash -xc 'git status'`,
		`bash --rcfile NUL -c 'echo safe'`,
		`bash --init-file NUL -ec 'git status'`,
		`bash "-c" "echo safe"`,
		`bash "--rcfile" NUL "-ec" "git status"`,
		`powershell -NoProfile -Command "Write-Output 'codex exec forbidden'"`,
		`codex --version`,
		`claude --version`,
	}
	for _, command := range allowed {
		if blocked, reason := classifyBypass(command, nil); blocked {
			t.Errorf("N130 safe diagnostic incorrectly rejected: %q (%s)", command, reason)
		}
	}
}

func TestN130NestedProcessWrappersAndCmdCombinedSwitchesFailClosed(t *testing.T) {
	denied := []string{
		"exec claude -p review",
		"command codex exec --json",
		"sudo -n claude -p review",
		"sudo -u reviewer codex exec --json",
		"timeout 5 claude -p review",
		"timeout -k 2 5 codex exec --json",
		`eval "claude -p review"`,
		"watch -n 1 codex exec --json",
		"watch -d -n 1 codex exec --json",
		"X=1 claude -p review",
		"X=1 codex exec --json",
		"sudo X=1 claude -p review",
		"sudo -nu reviewer claude -p review",
		"time -p claude -p review",
		"nice -n 2 codex exec --json",
		"flock -n lockfile claude -p review",
		`cmd /d/c "claude -p review"`,
		`cmd /s/c "codex exec --json"`,
		`cmd /c"claude -p review"`,
		`dash -c 'claude -p review'`,
		`ksh -c 'codex exec --json'`,
		`fish -c 'claude -p review'`,
		`busybox sh -c 'claude -p review'`,
		`toybox sh -c 'codex exec --json'`,
	}
	for _, s := range denied {
		if blocked, why := classifyBypass(s, nil); !blocked || why == "" {
			t.Errorf("nested vendor model launch escaped guard: %q (%s)", s, why)
		}
	}
	allowed := []string{
		"exec git status",
		"command git status",
		"sudo -n echo safe",
		"timeout 5 git status",
		`eval "echo safe"`,
		"watch -n 1 echo safe",
		"X=1 echo safe",
		`echo "claude -p review"`,
		`printf "codex exec --json"`,
		`rg "claude -p" docs`,
		`cmd /d/c "echo claude -p review"`,
		`cmd /d/c "echo safe"`,
		`cmd /c"echo safe"`,
		`dash -c 'git status'`,
		`busybox sh -c 'echo safe'`,
	}
	for _, s := range allowed {
		if blocked, why := classifyBypass(s, nil); blocked {
			t.Errorf("safe wrapped diagnostic incorrectly blocked: %q (%s)", s, why)
		}
	}
}

func TestN130PathShellOptionsFailClosedWithoutDiagnosticFalseDenials(t *testing.T) {
	denied := []string{
		`"C:\Program Files\PowerShell\7\pwsh.exe" -EncodedCommand YwBsAGEAdQBkAGUA`,
		`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -File C:\Temp\script.ps1`,
		`pwsh -enc YwBsAGEAdQBkAGUA`,
		`pwsh -ec YwBsAGEAdQBkAGUA`,
		`pwsh -EncodedC YwBsAGEAdQBkAGUA`,
		`powershell -Fi C:\Temp\script.ps1`,
		`pwsh -NoProfile -Comm "claude -p review"`,
		`pwsh -NoProfile -CommandWithArgs "codex exec --json"`,
		`pwsh -Comm:"codex exec --json"`,
		`/usr/bin/bash -o pipefail -c 'claude --print review'`,
		`/usr/bin/bash -O extglob -ec 'codex exec --json'`,
		`/usr/bin/bash +O extglob -ec 'codex exec --json'`,
		`/usr/bin/bash "+o" pipefail "-c" "claude -p hi"`,
		`/usr/bin/bash -Onocaseglob -ec 'claude -p hi'`,
	}
	for _, s := range denied {
		if blocked, why := classifyBypass(s, nil); !blocked || why == "" {
			t.Errorf("N130 additional nested launcher bypass: %q (%s)", s, why)
		}
	}
	allowed := []string{
		`powershell -NoProfile -Command "Write-Output '-File is a switch name'"`,
		`pwsh -Command "Write-Output '-EncodedCommand is a switch name'"`,
		`pwsh -NoProfile -Comm "Write-Output '-Fi is a switch name'"`,
		`pwsh -NoProfile -CommandWithArgs "git status"`,
		`pwsh -Comm:"Write-Output safe"`,
		`powershell -ExecutionPolicy RemoteSigned -Command "Write-Output safe"`,
		`/usr/bin/bash -o pipefail -c 'echo safe'`,
		`/usr/bin/bash -O extglob -ec 'git status'`,
		`/usr/bin/bash +O extglob -ec 'git status'`,
		`/usr/bin/bash -Onocaseglob -ec 'echo safe'`,
	}
	for _, s := range allowed {
		if blocked, why := classifyBypass(s, nil); blocked {
			t.Errorf("N130 safe diagnostic was wrongly denied: %q (%s)", s, why)
		}
	}
}
