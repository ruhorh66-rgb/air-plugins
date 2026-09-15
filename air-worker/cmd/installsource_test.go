package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Каталог конфигурации подменяется временным, права подаются параметром — ни сети, ни
// настоящей установки. Ниже собирается макет <config>\plugins\* и путь self в кэше.

func writeConfig(t *testing.T, cfg, marketplace, installedVersion string) {
	t.Helper()
	pluginsDir := filepath.Join(cfg, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	switch marketplace {
	case "github":
		mustWrite(t, filepath.Join(pluginsDir, "known_marketplaces.json"),
			`{"air-plugins":{"source":{"source":"github","repo":"ruhorh66-rgb/air-plugins"},"installLocation":"x"}}`)
	case "directory":
		mustWrite(t, filepath.Join(pluginsDir, "known_marketplaces.json"),
			`{"air-plugins":{"source":{"source":"directory","path":"F:\\-8-\\air-plugins"},"installLocation":"F:\\-8-\\air-plugins"}}`)
	case "none":
		// файла нет намеренно
	}
	if installedVersion != "" {
		mustWrite(t, filepath.Join(pluginsDir, "installed_plugins.json"),
			`{"version":2,"plugins":{"air-worker@air-plugins":[{"scope":"user","installPath":"p","version":"`+installedVersion+`"}]}}`)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cacheSelf — путь бинарника в каноническом кэше GitHub-маркетплейса.
func cacheSelf(cfg, ver string) string {
	return filepath.Join(cfg, "plugins", "cache", "air-plugins", "air-worker", ver, "bin", "air-worker.exe")
}

func TestИсточникGitHubКэшСтавит(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	g := decideInstallGuard(installGuard{
		Elevated: false, Self: cacheSelf(cfg, "0.10.1"), ConfigDir: cfg, SrcVersion: "0.10.1",
	})
	if !g.Allow {
		t.Fatalf("бинарник из кэша GitHub-маркетплейса обязан ставиться, а отказ: %v", g.Reasons)
	}
}

func TestИсточникКаталогМаркетплейсаОтказ(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "directory", "") // self в кэше, но маркетплейс — каталог
	g := decideInstallGuard(installGuard{
		Elevated: false, Self: cacheSelf(cfg, "0.10.1"), ConfigDir: cfg, SrcVersion: "0.10.1",
	})
	if g.Allow || g.Code != 2 {
		t.Fatalf("каталог-маркетплейс обязан отказать кодом 2, получено allow=%v code=%d", g.Allow, g.Code)
	}
	if !containsSub(g.Reasons, "не GitHub") {
		t.Errorf("причина отказа не называет источник: %v", g.Reasons)
	}
}

func TestИсточникРабочееДеревоОтказ(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	self := filepath.Join(`F:\-7-\air-worker\bin`, "air-worker.exe") // рабочее дерево
	g := decideInstallGuard(installGuard{
		Elevated: false, Self: self, ConfigDir: cfg, SrcVersion: "0.10.1",
	})
	if g.Allow || g.Code != 2 {
		t.Fatalf("рабочее дерево обязано отказать кодом 2, получено allow=%v code=%d", g.Allow, g.Code)
	}
}

func TestИсточникИзвлечёнИзТегаОтказ(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	self := filepath.Join(t.TempDir(), "air-worker--v0.10.0", "air-worker", "bin", "air-worker.exe")
	g := decideInstallGuard(installGuard{
		Elevated: false, Self: self, ConfigDir: cfg, SrcVersion: "0.10.0",
	})
	if g.Allow || g.Code != 2 {
		t.Fatalf("бинарник из тега обязан отказать кодом 2, получено allow=%v code=%d", g.Allow, g.Code)
	}
}

func TestПонижениеВерсииБезПравОтказ(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	g := decideInstallGuard(installGuard{
		Elevated: false, Self: cacheSelf(cfg, "0.9.4"), ConfigDir: cfg,
		SrcVersion: "0.9.4", DstVersion: "0.10.0", // ставим старее установленного
	})
	if g.Allow || g.Code != 2 {
		t.Fatalf("понижение без прав обязано отказать кодом 2, получено allow=%v code=%d", g.Allow, g.Code)
	}
	if !containsSub(g.Reasons, "0.10.0") || !containsSub(g.Reasons, "0.9.4") {
		t.Errorf("в отказе понижения нет обеих версий: %v", g.Reasons)
	}
}

func TestПонижениеВерсииСПравамиРазрешено(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	g := decideInstallGuard(installGuard{
		Elevated: true, Self: cacheSelf(cfg, "0.9.4"), ConfigDir: cfg,
		SrcVersion: "0.9.4", DstVersion: "0.10.0",
	})
	if !g.Allow {
		t.Fatalf("с правами понижение разрешено, а отказ: %v", g.Reasons)
	}
	if !containsSub(g.Info, "0.10.0") || !containsSub(g.Info, "0.9.4") {
		t.Errorf("с правами обе версии обязаны печататься: %v", g.Info)
	}
}

func TestПраваРазрешаютЛюбойИсточник(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "none", "")
	self := filepath.Join(`F:\-7-\air-worker\bin`, "air-worker.exe")
	g := decideInstallGuard(installGuard{
		Elevated: true, Self: self, ConfigDir: cfg, SrcVersion: "0.10.1",
	})
	if !g.Allow {
		t.Fatalf("с правами прямая переустановка из любого источника разрешена, а отказ: %v", g.Reasons)
	}
	if !containsSub(g.Info, "рабочее дерево") {
		t.Errorf("источник обязан печататься и с правами: %v", g.Info)
	}
}

func TestРазборKnownMarketplaces(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	mp, ok := readMarketplace(cfg, canonicalMarketplace)
	if !ok || !marketplaceIsGitHub(mp) {
		t.Fatalf("github-запись не разобрана: ok=%v mp=%+v", ok, mp)
	}
	// каталог не считается GitHub
	cfg2 := t.TempDir()
	writeConfig(t, cfg2, "directory", "")
	mp2, ok2 := readMarketplace(cfg2, canonicalMarketplace)
	if !ok2 || marketplaceIsGitHub(mp2) {
		t.Fatalf("каталог принят за GitHub: ok=%v mp=%+v", ok2, mp2)
	}
}

func TestРазборInstalledPlugins(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "0.10.0")
	rec, ok := readInstalledPlugin(cfg, canonicalPluginKey)
	if !ok || rec.Version != "0.10.0" {
		t.Fatalf("регистрация плагина не разобрана: ok=%v rec=%+v", ok, rec)
	}
	if _, ok := readInstalledPlugin(t.TempDir(), canonicalPluginKey); ok {
		t.Error("пустой конфиг не должен давать регистрацию")
	}
}

func TestПользовательскаяКопияСкила(t *testing.T) {
	cfg := t.TempDir()
	if p, shadow := userSkillShadow(cfg); shadow {
		t.Fatalf("на пустом конфиге тени скила быть не должно: %s", p)
	}
	if err := os.MkdirAll(filepath.Join(cfg, "skills", "air-woody"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, shadow := userSkillShadow(cfg)
	if !shadow || !strings.Contains(p, "air-woody") {
		t.Fatalf("пользовательская копия скила не найдена: shadow=%v p=%s", shadow, p)
	}
}

func TestInstallStatusВидитНарушения(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "directory", "") // каталог-маркетплейс, плагин не зарегистрирован
	if err := os.MkdirAll(filepath.Join(cfg, "skills", "woody"), 0o755); err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(`F:\-7-\air-worker\bin`, "air-worker.exe")
	// плагин не зарегистрирован — сверять SHA-256 не с чем; сам installedPath не читается.
	installedPath := filepath.Join(t.TempDir(), "air-worker", "bin", "air-worker.exe")
	lines, violations := installSourceStatusLines(cfg, installedPath, self)
	if len(lines) == 0 {
		t.Fatal("статус не назвал ни одной строки источника")
	}
	// каждое из трёх нарушений — отдельной строкой
	want := []string{"не GitHub", "не зарегистрирован", "скила"}
	for _, w := range want {
		if !containsSub(violations, w) {
			t.Errorf("нарушение %q не названо отдельной строкой: %v", w, violations)
		}
	}
	if !containsSub(violations, "marketplace remove air-plugins") {
		t.Errorf("нарушение маркетплейса не несёт недостающих штатных команд: %v", violations)
	}
}

// writeCachedPlugin — регистрирует air-worker@air-plugins в installed_plugins.json с
// installPath на каталог кэша и кладёт туда bin\air-worker.exe заданного содержимого.
// Возвращает путь к этому файлу кэша.
func writeCachedPlugin(t *testing.T, cfg, version string, content []byte) string {
	t.Helper()
	cacheDir := filepath.Join(cfg, "plugins", "cache", "air-plugins", "air-worker", version)
	cacheBin := filepath.Join(cacheDir, "bin", "air-worker.exe")
	if err := os.MkdirAll(filepath.Dir(cacheBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheBin, content, 0o644); err != nil {
		t.Fatal(err)
	}
	pluginsDir := filepath.Join(cfg, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":2,"plugins":{"air-worker@air-plugins":[{"scope":"user","installPath":` +
		strconv.Quote(cacheDir) + `,"version":"` + version + `"}]}}`
	mustWrite(t, filepath.Join(pluginsDir, "installed_plugins.json"), body)
	return cacheBin
}

// СТАТУС ОБЯЗАН СУДИТЬ УСТАНОВЛЕННЫЙ ФАЙЛ, А НЕ ЗАПУЩЕННЫЙ. `air-worker install -status`
// обычно вызывают из PATH — то есть самой установленной копией, — и self тогда равен
// installedPath, а не кэшу. До фикса это давало «НАРУШЕНИЕ: запущенный бинарник не из
// кэша» ВСЕГДА, даже при полностью штатной установке.
func TestStatusИсточникУстановленногоСовпалСКэшем(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	content := []byte("один и тот же файл")
	writeCachedPlugin(t, cfg, "0.10.1", content)

	installedPath := filepath.Join(t.TempDir(), "air-worker", "bin", "air-worker.exe")
	if err := os.MkdirAll(filepath.Dir(installedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// self — установленная копия, вызванная через PATH: НЕ в кэше. Нарушений всё равно
	// быть не должно, потому что судится installedPath, а не self.
	self := installedPath
	lines, violations := installSourceStatusLines(cfg, installedPath, self)
	if len(violations) != 0 {
		t.Fatalf("установленный файл совпал с кэшем, а нарушения есть: %v", violations)
	}
	if !containsSub(lines, "SHA-256 совпадает") {
		t.Errorf("строка про совпадение SHA-256 не напечатана: %v", lines)
	}
}

func TestStatusИсточникУстановленногоНеСовпалСКэшем(t *testing.T) {
	cfg := t.TempDir()
	writeConfig(t, cfg, "github", "")
	writeCachedPlugin(t, cfg, "0.10.1", []byte("содержимое кэша"))

	installedPath := filepath.Join(t.TempDir(), "air-worker", "bin", "air-worker.exe")
	if err := os.MkdirAll(filepath.Dir(installedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath, []byte("подменённое содержимое"), 0o644); err != nil {
		t.Fatal(err)
	}

	self := installedPath
	_, violations := installSourceStatusLines(cfg, installedPath, self)
	if !containsSub(violations, "SHA-256") {
		t.Errorf("расхождение SHA-256 установленного файла с кэшем не дало нарушения: %v", violations)
	}
}

// TestSamePathЧерезJunction — на этой машине каталоги плагина часто связаны junction, и
// сравнение голых строк тогда врёт: путь через junction и его цель — один каталог на
// диске, но разные строки. samePath обязана разрешать ссылки с обеих сторон.
//
// Junction создаётся БЕЗ прав администратора (в отличие от symlink) — `mklink /J` их не
// требует. Если создать всё равно не вышло (политика машины, файловая система без
// reparse point), тест пропускается с причиной, а не валит сборку.
func TestSamePathЧерезJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction — понятие Windows")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput()
	if err != nil {
		t.Skipf("junction не создана без прав на этой машине: %v — %s", err, string(out))
	}

	if !samePath(link, real) {
		t.Errorf("samePath не свела junction %q и цель %q к одному пути", link, real)
	}
	if samePath(link, filepath.Join(base, "нет-такого-каталога")) {
		t.Error("samePath обязана отличать junction от несуществующего каталога, а не совпадать со всем подряд")
	}
}

func TestСверкаSHA256(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	c := filepath.Join(dir, "c.bin")
	mustWrite(t, a, "одни и те же байты")
	mustWrite(t, b, "одни и те же байты")
	mustWrite(t, c, "другие байты")
	ha, _ := sha256File(a)
	hb, _ := sha256File(b)
	hc, _ := sha256File(c)
	if ha == "" || ha != hb {
		t.Errorf("равные файлы дали разный SHA-256: %s vs %s", ha, hb)
	}
	if ha == hc {
		t.Errorf("разные файлы дали равный SHA-256: %s", ha)
	}
	if _, err := sha256File(filepath.Join(dir, "нет.bin")); err == nil {
		t.Error("несуществующий файл обязан дать ошибку, а не пустой хэш без ошибки")
	}
}

func TestСравнениеВерсий(t *testing.T) {
	cases := []struct {
		a, b string
		want int // знак
	}{
		{"0.10.0", "0.9.4", 1}, // 10 > 9 по полю, не по строке
		{"0.9.4", "0.10.0", -1},
		{"0.10.1", "0.10.1", 0},
		{"0.10.1", "0.10.0", 1},
		{"1.0.0", "1.0.0-beta.1", 1}, // предвыпуск старше
		{"1.0.0-beta.1", "1.0.0", -1},
		{"v0.10.1", "0.10.1", 0}, // ведущая v не мешает
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if sign(got) != c.want {
			t.Errorf("compareVersions(%q,%q)=%d, ожидался знак %d", c.a, c.b, got, c.want)
		}
	}
}

// КОД ОТКАЗА ДОХОДИТ ДО ПРОЦЕССА. Проверяется прогоном самого процесса: тестовый
// бинарник перезапускает себя как CLI (шов run() → os.Exit в main), install из чужого
// источника обязан завершить ПРОЦЕСС кодом 2. AIR-ENV-002 14.09.2026: такой отказ дошёл
// нулём — код терялся снаружи Go; этот тест закрывает шов на стороне бинарника.
func TestКодОтказаДоходитДоПроцесса(t *testing.T) {
	if os.Getenv("AW_RUN_HELPER") == "1" {
		os.Exit(run(splitUnit(os.Getenv("AW_RUN_ARGS"))))
		return
	}
	cfg := t.TempDir()  // пустой конфиг: маркетплейса нет, self не в кэше → отказ 2
	home := t.TempDir() // некуда ставить всё равно не дойдёт
	cmd := exec.Command(os.Args[0], "-test.run=TestКодОтказаДоходитДоПроцесса")
	cmd.Env = append(cleanEnv("AW_RUN_HELPER", "AW_RUN_ARGS", "CLAUDE_CONFIG_DIR", "AIR_WORKER_HOME"),
		"AW_RUN_HELPER=1",
		"AW_RUN_ARGS="+strings.Join([]string{"install", "-dir", home, "-no-start"}, "\x1f"),
		"CLAUDE_CONFIG_DIR="+cfg,
		"AIR_WORKER_HOME="+home,
	)
	err := cmd.Run()
	if code := exitCode(cmd, err); code != 2 {
		t.Fatalf("отказ установки обязан дойти до процесса кодом 2, а дошёл %d", code)
	}
}

func TestRunВозвращаетКодПодкоманды(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Errorf("version должен вернуть 0, вернул %d", code)
	}
	if code := run(nil); code != 2 {
		t.Errorf("без подкоманды ожидался 2, вернул %d", code)
	}
	if code := run([]string{"чего-то-нет"}); code != 2 {
		t.Errorf("неизвестная подкоманда должна вернуть 2, вернул %d", code)
	}
}

// --- мелочи -----------------------------------------------------------------

func containsSub(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

func splitUnit(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x1f")
}

func cleanEnv(drop ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		keep := true
		for _, d := range drop {
			if strings.HasPrefix(strings.ToUpper(kv), strings.ToUpper(d)+"=") {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}
