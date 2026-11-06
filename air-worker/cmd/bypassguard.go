package main

// СТРАЖ ОБХОДА — ЧИСТАЯ ФУНКЦИЯ. По тексту команды оболочки она решает, обход это
// штатного пути установки или нет. Ничего не запускает и не читает диск: решение
// принимается только по самой строке (и параметру путей установки), поэтому его
// проверяют набором команд.
//
// Обходом считается ровно три вещи (решение ЛПР 14.09.2026, шаг 66):
//   1. `claude plugin marketplace add` с ЛОКАЛЬНЫМ путём вместо GitHub owner/repo;
//   2. копирование или перемещение файла В КАТАЛОГ УСТАНОВКИ air-worker либо в кэш
//      плагина (plugins\cache);
//   3. запуск `air-worker.exe install` НЕ из кэша плагина.
//
// Правило 2 сузили: раньше обходом считалась ЛЮБАЯ команда копирования или перемещения,
// где в строке встречается «air-worker.exe» — и под это подпадали законные команды:
// копия бинарника из рабочего дерева в черновой каталог, сборка выпуска в bin/,
// резервная копия. Обход — только когда путь ЦЕЛИ (или вообще команды) лежит под
// каталогом установки или под кэшем плагина: только туда копия действительно подменяет
// штатно поставленный файл.
//
// Пути каталога установки — ПАРАМЕТР, не константа и не вызов installHome(): функция
// остаётся чистой, а не привязанной к ОС или моменту установки.
//
// Подключение этой функции к событию хоста — шаг 38 (диспетчер хуков); здесь только
// само решение. В hooks.json на этом шаге ничего не добавляется.

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reMarketplaceAdd = regexp.MustCompile(`(?i)\bplugin\s+marketplace\s+add\s+([^\r\n;&|]+)`)
	reOwnerRepo      = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	reCopyVerb       = regexp.MustCompile(`(?i)(^|[\s|&;])(copy|xcopy|robocopy|move|copy-item|move-item|cp|mv)([\s]|$)`)
	reAWInstall      = regexp.MustCompile(`(?i)("[^"]*air-worker\.exe"|\S*air-worker\.exe|air-worker)\s+install\b`)
	// Only an actual shell command position, not quoted documentation or grep
	// pattern text. Active product sessions must call native AirWorker routes.
	reDirectNativeModel = regexp.MustCompile(`(?im)(?:^|[\r\n;&|])\s*(?:&\s*)?(?:(?:"[^"\r\n]*[\\/]|'[^'\r\n]*[\\/])|(?:[^\s"';&|\r\n]*[\\/]))?(codex(?:\.(?:cmd|exe|ps1))?|claude(?:\.(?:cmd|exe|ps1))?)(?:["'])?\s+([^\r\n;&|]+)`)
)

// classifyBypass возвращает (обход, причина). Причина непустая только при обходе.
// installDirs — путь(и) каталога установки air-worker (например, %LOCALAPPDATA%\air-worker
// в разрешённом виде); пустые элементы и пустой срез допустимы — тогда правило 2 ловит
// только копии в кэш плагина.
func classifyBypass(cmdText string, installDirs []string) (bool, string) {
	return classifyBypassDepth(cmdText, installDirs, 0)
}

func classifyBypassDepth(cmdText string, installDirs []string, depth int) (bool, string) {
	s := strings.TrimSpace(cmdText)
	if s == "" {
		return false, ""
	}

	// 1) An authorized GitHub marketplace operation is not a blanket ALLOW
	// for the rest of a compound shell line. Check *every* marketplace add
	// and continue to the copy/install/direct-model guards below.
	for _, m := range reMarketplaceAdd.FindAllStringSubmatch(s, -1) {
		arg, found := marketplaceSourceArg(m[1])
		if !found || isLocalPathArg(arg) {
			return true, "подмена маркетплейса локальным или неразобранным источником: " + arg + " — источник обязан быть GitHub ruhorh66-rgb/air-plugins"
		}
	}

	// 2) копирование/перемещение в каталог установки air-worker или в кэш плагина.
	if reCopyVerb.MatchString(s) {
		lowSlash := strings.ToLower(filepath.ToSlash(s))
		for _, dir := range installDirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if strings.Contains(lowSlash, strings.ToLower(filepath.ToSlash(dir))) {
				return true, "копирование или перемещение в каталог установки air-worker (" + dir + ") — установка идёт только из кэша плагина"
			}
		}
		if strings.Contains(lowSlash, "plugins/cache") {
			return true, "копирование или перемещение в кэш плагина — установка идёт только штатным путём"
		}
	}

	// 3) запуск air-worker.exe install не из кэша плагина.
	if p, isInstall := airWorkerInstallInvocation(s); isInstall {
		if p != "" && !underPluginCache(p) {
			return true, "запуск air-worker.exe install не из кэша плагина: " + p
		}
	}

	// 4) An active AirWorker session must not start a model or infer Claude
	// availability through a naked vendor CLI. These patterns are scoped to
	// shell-command positions; they do not scan documentation or ordinary text.
	for _, m := range reDirectNativeModel.FindAllStringSubmatch(s, -1) {
		tool := strings.ToLower(m[1])
		args := strings.Fields(strings.ToLower(m[2]))
		if len(args) == 0 {
			continue
		}
		// Shell quotes are not part of the executed argv. A quoted "-p",
		// "exec" or "app-server" has the same model-launch behavior as
		// its unquoted spelling; keep safe --version diagnostics allowed.
		for i := range args {
			args[i] = strings.Trim(args[i], "\"'")
		}
		if strings.HasPrefix(tool, "codex") {
			for _, arg := range args {
				if arg == "exec" || arg == "e" || arg == "app-server" {
					return true, "прямой Codex исполняет модель вне AirWorker: используй air-worker orchestrate для двух субагентов или штатный executor для строго одиночного задания"
				}
			}
		}
		if !strings.HasPrefix(tool, "claude") {
			continue
		}
		if len(args) > 1 && args[0] == "auth" && args[1] == "status" {
			return true, "голый claude auth status не проверяет user-env OAuth и Headroom AirWorker: используй air-worker tool -which claude -preflight -json; реальный вызов подтверждай native judge receipt"
		}
		for _, arg := range args {
			if arg == "-p" || arg == "--print" {
				return true, "прямой Claude reviewer обходит native AirWorker и квитанцию независимого судьи: используй air-worker semantic или plan-review"
			}
		}
	}
	// Direct regexes correctly avoid quoted documentation and grep output.
	// Shell interpreters, however, execute a nested command string. Unwrap
	// only explicit launcher syntax and apply the SAME native guard there.
	if depth < 6 {
		if blocked, reason := classifyKnownShellWrapper(s, installDirs, depth+1); blocked {
			return true, reason
		}
	}
	return false, ""
}

// isLocalPathArg — аргумент `marketplace add` есть локальный путь, а не GitHub owner/repo.
// GitHub-форма `owner/repo` содержит один слэш и обе части словарные; всё остальное с
// разделителем пути, буквой диска, ведущими ./ ../ ~ / или обратным слэшем — локальный
// путь. URL http(s) не локальный (это удалённый источник, отдельный от каталога).
// Marketplace flags must not hide a local source. The one supported
// split-value option is --scope; unknown flag values are fail-closed.
func marketplaceSourceArg(rest string) (string, bool) {
	args := strings.Fields(strings.TrimSpace(rest))
	for i := 0; i < len(args); i++ {
		arg := strings.Trim(args[i], `"'`)
		if strings.HasPrefix(arg, "-") {
			if arg == "--scope" || arg == "-s" {
				i++
			}
			continue
		}
		return arg, true
	}
	return "", false
}

func isLocalPathArg(a string) bool {
	a = strings.Trim(a, `"'`)
	if a == "" {
		return false
	}
	if strings.Contains(a, `\`) {
		return true // путь Windows
	}
	if len(a) >= 2 && a[1] == ':' && isAlphaByte(a[0]) {
		return true // C:\... или C:/...
	}
	if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "./") ||
		strings.HasPrefix(a, "../") || strings.HasPrefix(a, "~") || strings.HasPrefix(a, "file:") {
		return true
	}
	lo := strings.ToLower(a)
	if strings.HasPrefix(lo, "http://") || strings.HasPrefix(lo, "https://") {
		return !strings.HasPrefix(lo, "https://github.com/") // only trusted GitHub HTTPS can be a marketplace URL
	}
	if reOwnerRepo.MatchString(a) {
		parts := strings.SplitN(a, "/", 2)
		if parts[0] != "." && parts[0] != ".." && parts[1] != "." && parts[1] != ".." {
			return false // GitHub owner/repo, no dot-component path alias
		}
	}
	// Unknown bare tokens and all non-GitHub path syntaxes fail closed.
	return true
}

func isAlphaByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// airWorkerInstallInvocation — это ли запуск `... air-worker.exe install`, и путь к exe.
// Путь пустой означает «указано голым именем» (через PATH): откуда именно оно
// разрешится, из строки не видно, и такой случай обходом не объявляется.
func airWorkerInstallInvocation(s string) (string, bool) {
	m := reAWInstall.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	p := strings.Trim(m[1], `"'`)
	if !strings.ContainsAny(p, `/\`) {
		return "", true // голое имя, путь неизвестен
	}
	return filepath.Clean(p), true
}

var (
	reKnownShellSegment       = regexp.MustCompile(`(?m)(?:^|[\r\n;&|])\s*([^\r\n;&|]+)`)
	reShellSubstitution       = regexp.MustCompile(`\$\(([^)]{1,512})\)`)
	reShellBacktick           = regexp.MustCompile("`([^`\\r\\n]{1,512})`")
	reShellParenthesizedModel = regexp.MustCompile(`(?i)\(\s*((?:codex|claude)(?:\.(?:cmd|exe|ps1))?\s+[^)]{1,512})\)`)
)

// Last-resort check for an indirectly wrapped vendor model invocation.
// A shell string is NOT an executable AST: this intentionally conservative
// guard catches clear CLI+model-flag argv words after unrecognised wrappers,
// while leaving explicit inert text emitters/searches alone. Never claim that
// a string check alone proves complete OS-level process enforcement.
func classifyIndirectVendorModelLaunch(command string) (bool, string) {
	argv := strings.Fields(strings.TrimSpace(command))
	if len(argv) < 2 {
		return false, ""
	}
	// POSIX NAME=value prefixes are assignments, not command executables.
	for len(argv) > 0 {
		word := strings.Trim(argv[0], "\"'")
		eq := strings.IndexByte(word, '=')
		if eq <= 0 || strings.ContainsAny(word[:eq], "/\\ -") {
			break
		}
		argv = argv[1:]
	}
	if len(argv) < 2 {
		return false, ""
	}
	first := strings.ToLower(strings.Trim(argv[0], "\"'"))
	// Git's built-in read-only grep reports literal model command strings;
	// it does not execute them. Do not exempt arbitrary Git aliases.
	if first == "git" && len(argv) > 1 && strings.EqualFold(argv[1], "grep") {
		return false, ""
	}
	switch first {
	case "echo", "printf", "grep", "rg", "findstr", "write-output",
		"select-string", "get-content", "cat", "type", "more", "head", "tail":
		return false, ""
	}
	for i, raw := range argv {
		token := strings.Trim(strings.ToLower(raw), "\"'(){}")
		base := path.Base(strings.ReplaceAll(token, "\\", "/"))
		typeOf := ""
		switch base {
		case "claude", "claude.exe", "claude.cmd", "claude.ps1":
			typeOf = "claude"
		case "codex", "codex.exe", "codex.cmd", "codex.ps1":
			typeOf = "codex"
		}
		if typeOf == "" {
			continue
		}
		// Model flags can follow many legitimate vendor options. A fixed
		// lookahead silently permits longer direct print/exec invocations.
		for j := i + 1; j < len(argv); j++ {
			arg := strings.Trim(strings.ToLower(argv[j]), "\"'(){}")
			if typeOf == "claude" && (arg == "-p" || arg == "--print") {
				return true, "косвенный прямой вызов Claude вне AirWorker: используй штатный review source"
			}
			if typeOf == "codex" && (arg == "exec" || arg == "e" || arg == "app-server") {
				return true, "косвенный прямой вызов Codex вне AirWorker: используй штатный orchestrate/executor"
			}
		}
	}
	return false, ""
}

// The host guard receives shell text, not an executable AST. Only wrappers
// that explicitly execute their argument as a command are unwrapped here.
// Plain grep, echo and Write-Output text remains inert.
func classifyKnownShellWrapper(command string, installDirs []string, depth int) (bool, string) {
	for _, m := range reKnownShellSegment.FindAllStringSubmatch(command, -1) {
		segment := strings.TrimSpace(m[1])
		if opaqueNativeShellLauncher(segment) {
			return true, "непрозрачный запуск PowerShell -EncodedCommand/-File невозможно проверить: используй штатный AirWorker вместо обхода native model guard"
		}
		payload := unwrapNativeShellLauncher(segment)
		if payload == "" || strings.EqualFold(payload, segment) {
			if blocked, reason := classifyIndirectVendorModelLaunch(segment); blocked {
				return true, reason
			}
			continue
		}
		if blocked, reason := classifyBypassDepth(payload, installDirs, depth); blocked {
			return true, reason
		}
		if blocked, reason := classifyIndirectVendorModelLaunch(payload); blocked {
			return true, reason
		}
	}
	// Shell command substitutions run an inner command even when the outer
	// program is an otherwise harmless echo or a shell conditional.
	for _, pattern := range []*regexp.Regexp{
		reShellSubstitution, reShellBacktick, reShellParenthesizedModel,
	} {
		for _, m := range pattern.FindAllStringSubmatch(command, -1) {
			if blocked, reason := classifyBypassDepth(strings.TrimSpace(m[1]), installDirs, depth); blocked {
				return true, reason
			}
		}
	}
	return false, ""
}

func unquoteCommandPayload(s string) string {
	s = strings.TrimSpace(s)
	for count := 0; count < 4; count++ {
		if len(s) >= 3 && strings.HasPrefix(s, "$(") && strings.HasSuffix(s, ")") {
			s = strings.TrimSpace(s[2 : len(s)-1])
		} else if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
			s = strings.TrimSpace(s[1 : len(s)-1])
		} else if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' ||
			s[0] == '\'' && s[len(s)-1] == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		} else {
			break
		}
	}
	return s
}

// nativeShellLauncherArgs separates an executable from arguments without
// splitting a quoted Windows path such as "C:\Program Files\PowerShell\7\pwsh.exe".
// It only inspects shell text; it never resolves or executes that path.
func nativeShellLauncherArgs(segment string) (string, []string, bool) {
	s := strings.TrimSpace(segment)
	if strings.HasPrefix(s, "& ") {
		s = strings.TrimSpace(s[1:])
	}
	if s == "" {
		return "", nil, false
	}
	var executable, rest string
	quoted := s[0] == '"' || s[0] == '\''
	if quoted {
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 {
			return "", nil, false
		}
		executable = s[1 : end+1]
		rest = strings.TrimSpace(s[end+2:])
	} else {
		end := strings.IndexAny(s, " \t\r\n")
		if end < 0 {
			return "", nil, false
		}
		executable = s[:end]
		rest = strings.TrimSpace(s[end+1:])
	}
	base := strings.ToLower(path.Base(strings.ReplaceAll(executable, "\\", "/")))
	return base, strings.Fields(rest), quoted
}

// nativePowerShellLauncherOption recognizes only command-producing or opaque
// PowerShell launcher options, including documented aliases and unambiguous
// long-option prefixes. ExecutionPolicy/NoProfile and diagnostic text remain
// ordinary options. No shell code or files are executed by this classifier.
func nativePowerShellLauncherOption(raw string) (kind, inlineValue string) {
	token := strings.Trim(raw, "\"'")
	if !strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "/") {
		return "", ""
	}
	option := strings.TrimLeft(token, "-/")
	value := ""
	if cut := strings.IndexAny(option, ":="); cut >= 0 {
		value = option[cut+1:]
		option = option[:cut]
	}
	option = strings.ToLower(option)
	switch {
	case option == "e" || option == "ec" ||
		(len(option) >= 2 && (strings.HasPrefix("encodedcommand", option) ||
			strings.HasPrefix("encodedarguments", option))):
		return "opaque", ""
	case option == "f" || (len(option) >= 2 && strings.HasPrefix("file", option)):
		return "opaque", ""
	case option == "c" || (len(option) >= 2 &&
		(strings.HasPrefix("command", option) || strings.HasPrefix("commandwithargs", option))):
		return "command", value
	default:
		return "", ""
	}
}

// Opaque PowerShell inputs cannot be inspected without running untrusted
// scripts or decoding arbitrary payloads. Reject the launcher before -Command,
// not quoted data AFTER -Command (e.g. Write-Output '-File is a switch name').
func opaqueNativeShellLauncher(segment string) bool {
	first, args, _ := nativeShellLauncherArgs(segment)
	if first != "powershell" && first != "powershell.exe" &&
		first != "pwsh" && first != "pwsh.exe" {
		return false
	}
	for _, raw := range args {
		kind, _ := nativePowerShellLauncherOption(raw)
		switch kind {
		case "command":
			return false
		case "opaque":
			return true
		}
	}
	return false
}

func unwrapNativeShellLauncher(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	if (strings.HasPrefix(segment, "$(") || strings.HasPrefix(segment, "(")) &&
		strings.HasSuffix(segment, ")") {
		return unquoteCommandPayload(segment)
	}
	first, args, executableQuoted := nativeShellLauncherArgs(segment)
	if len(args) == 0 {
		return ""
	}
	joinAfter := func(i int) string {
		if i >= len(args) {
			return ""
		}
		return unquoteCommandPayload(strings.Join(args[i:], " "))
	}
	switch first {
	case "cmd", "cmd.exe":
		for i, arg := range args {
			low := strings.ToLower(arg)
			if !strings.HasPrefix(strings.TrimLeft(low, "\"'"), "/") {
				continue
			}
			// cmd.exe accepts combined switches (/d/c, /s/c) and an
			// attached command after /c, e.g. /c"claude -p ...".
			for j := 0; j+1 < len(low); j++ {
				if low[j] != '/' || (low[j+1] != 'c' && low[j+1] != 'k') {
					continue
				}
				if j+2 < len(low) && low[j+2] != '/' &&
					low[j+2] != '"' && low[j+2] != '\'' {
					continue
				}
				payload := arg[j+2:]
				if payload == "" {
					return joinAfter(i + 1)
				}
				if i+1 < len(args) {
					payload += " " + strings.Join(args[i+1:], " ")
				}
				return unquoteCommandPayload(payload)
			}
		}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		for i := 0; i < len(args); i++ {
			kind, inlineValue := nativePowerShellLauncherOption(args[i])
			if kind != "command" {
				continue
			}
			if inlineValue != "" {
				payload := inlineValue
				if i+1 < len(args) {
					payload += " " + strings.Join(args[i+1:], " ")
				}
				return unquoteCommandPayload(payload)
			}
			return joinAfter(i + 1)
		}
	case "bash", "sh", "zsh", "dash", "ksh", "mksh",
		"fish", "csh", "tcsh", "ash":
		for i := 0; i < len(args); {
			// Shell quotes around an argv option do not change how Bash
			// parses it: "-c", '-ec' and "--rcfile" are still options.
			rawOption := strings.Trim(args[i], "\"'")
			option := strings.ToLower(rawOption)
			if option == "--" || (!strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "+")) {
				break
			}
			// Bash -o/-O each consume an option name as the next argument.
			// Skip that name before looking for a later -c or combined -ec.
			if rawOption == "-o" || rawOption == "-O" ||
				rawOption == "+o" || rawOption == "+O" {
				if i+1 >= len(args) {
					return ""
				}
				i += 2
				continue
			}
			// An attached value belongs to -o/-O or +o/+O; its letters
			// (including a 'c' in nocaseglob) are not combined shell flags.
			if len(rawOption) > 2 && (strings.HasPrefix(rawOption, "-o") ||
				strings.HasPrefix(rawOption, "-O") ||
				strings.HasPrefix(rawOption, "+o") ||
				strings.HasPrefix(rawOption, "+O")) {
				i++
				continue
			}
			// GNU Bash --rcfile and --init-file each consume the next
			// filename token. Do not interpret it as a script positional
			// argument and overlook a later -c/-ec.
			if option == "--rcfile" || option == "--init-file" {
				if i+1 >= len(args) {
					return ""
				}
				i += 2
				continue
			}
			// Bash allows combined short flags, e.g. -ec, -xc, -lc.
			// The -c flag consumes the following argument as a command.
			if len(option) > 1 && strings.HasPrefix(option, "-") &&
				!strings.HasPrefix(option, "--") && strings.Contains(option[1:], "c") {
				return joinAfter(i + 1)
			}
			i++
		}
	case "env":
		// env options precede the command and some consume a separate value.
		// Do not mistake those values for the executable being launched.
		for i := 0; i < len(args); {
			option := strings.ToLower(args[i])
			switch {
			case option == "--":
				return joinAfter(i + 1)
			case option == "-i" || option == "--ignore-environment":
				i++
			case option == "-u" || option == "--unset" || option == "-c" || option == "--chdir":
				if i+1 >= len(args) {
					return ""
				}
				i += 2
			case strings.HasPrefix(option, "--unset=") || strings.HasPrefix(option, "--chdir=") ||
				(strings.HasPrefix(option, "-u") && len(option) > 2) ||
				(strings.HasPrefix(option, "-c") && len(option) > 2):
				i++
			case option == "-s" || option == "--split-string":
				if i+1 >= len(args) {
					return ""
				}
				return strings.ReplaceAll(joinAfter(i+1), `\"`, `"`)
			case strings.HasPrefix(option, "--split-string="):
				payload := strings.TrimPrefix(args[i], "--split-string=")
				if i+1 < len(args) {
					payload += " " + strings.Join(args[i+1:], " ")
				}
				return strings.ReplaceAll(unquoteCommandPayload(payload), `\"`, `"`)
			case !strings.HasPrefix(option, "-") && strings.Contains(args[i], "="):
				i++
			default:
				return joinAfter(i)
			}
		}
		return ""
	case "call", "start", "then", "do", "nohup", "time", "{",
		"exec", "command", "builtin", "eval":
		i := 0
		if first == "start" && i < len(args) && (args[i] == `""` || args[i] == `''`) {
			i++
		}
		if first == "exec" || first == "command" || first == "builtin" {
			for i < len(args) && strings.HasPrefix(args[i], "-") {
				if args[i] == "--" {
					i++
					break
				}
				if first == "exec" && args[i] == "-a" {
					i += 2
				} else {
					i++
				}
			}
		}
		return joinAfter(i)
	case "sudo":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			option := strings.ToLower(args[i])
			if option == "--" {
				i++
				break
			}
			switch option {
			case "-u", "--user", "-g", "--group", "-h", "--host",
				"-p", "--prompt", "-c", "--close-from", "-r", "--role", "-t", "--type":
				i += 2
			default:
				i++
			}
		}
		return joinAfter(i)
	case "timeout":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			option := strings.ToLower(args[i])
			if option == "--" {
				i++
				break
			}
			switch option {
			case "-k", "--kill-after", "-s", "--signal":
				i += 2
			default:
				i++
			}
		}
		// Coreutils timeout consumes a duration before the command.
		return joinAfter(i + 1)
	case "watch":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			option := strings.ToLower(args[i])
			if option == "--" {
				i++
				break
			}
			if option == "-n" || option == "--interval" {
				i += 2
			} else {
				i++
			}
		}
		return joinAfter(i)
	case "busybox", "toybox":
		// The multicall executable receives "sh -c ..." as its argv.
		return joinAfter(0)
	case "xargs":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			switch args[i] {
			case "-n", "-L", "-P", "-I":
				i += 2
			default:
				i++
			}
		}
		return joinAfter(i)
	case "npx", "npx.cmd":
		i := 0
		for i < len(args) && (args[i] == "-y" || args[i] == "--yes") {
			i++
		}
		if i < len(args) {
			switch strings.ToLower(strings.Trim(args[i], `"'`)) {
			case "@openai/codex":
				return "codex " + strings.Join(args[i+1:], " ")
			case "@anthropic-ai/claude-code":
				return "claude " + strings.Join(args[i+1:], " ")
			}
		}
	case "codex", "claude", "codex.exe", "claude.exe", "codex.cmd", "claude.cmd":
		if executableQuoted {
			tool := strings.SplitN(first, ".", 2)[0]
			return tool + " " + strings.Join(args, " ")
		}
	}
	return ""
}
