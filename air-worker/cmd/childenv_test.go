package main

import (
	"strings"
	"testing"
)

func TestChildEnvironmentSanitizesPSModulePath(t *testing.T) {
	input := []string{
		`Path=C:\bin`,
		`PSMODULEPATH=C:\Windows\System32\WindowsPowerShell\v1.0\Modules;C:\Program Files\PowerShell\7\Modules;D:/tools/pwsh/7/Modules;C:\custom\Modules`,
		`Unrelated=value;with=equals`,
	}
	got := sanitizePSModulePath(input)
	if len(got) != len(input) || got[0] != input[0] || got[2] != input[2] {
		t.Fatalf("unrelated environment changed: %#v", got)
	}
	if !strings.Contains(got[1], `v1.0\Modules`) || !strings.Contains(got[1], `C:\custom\Modules`) {
		t.Fatalf("Windows PowerShell and custom modules must remain: %q", got[1])
	}
	if strings.Contains(strings.ToLower(got[1]), `\powershell\7`) || strings.Contains(strings.ToLower(got[1]), `\pwsh\7`) {
		t.Fatalf("PowerShell 7 module roots remain: %q", got[1])
	}
}
