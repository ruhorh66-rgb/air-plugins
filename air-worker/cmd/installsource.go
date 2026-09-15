package main

// ИСТОЧНИК УСТАНОВКИ — ТОЛЬКО ШТАТНЫЙ ПУТЬ ИЗ GitHub. Решение ЛПР 14.09.2026:
// «Только один путь верный — установка из GitHub… все другие попытки обновления
// неприемлемы… чтобы каждая сессия не правила код и не заливала то, чего ей хочется».
//
// Здесь ЧИСТАЯ логика решения об источнике, понижении версии и правах: она не зовёт
// Windows-API и не запускает процессов, поэтому её проверяют тестом на подставном
// каталоге конфигурации и с правами-параметром. Всё, что зависит от ОС (реальный
// токен прав, запуск установленного бинарника за версией), живёт в install_windows.go
// и подаёт сюда готовые значения.
//
// Нашла AIR-ENV-002 14.09.2026: после правки settings.json маркетплейс air-plugins
// остался КАТАЛОГОМ, `claude plugin update` ответил «уже последняя версия (0.9.4)»
// кодом 0, и установка из кэша молча откатила машину с 0.10.0 на 0.9.4. Отсюда два
// независимых рубежа — источник и понижение, — и каждый отказывает КОДОМ, а не строкой.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Канон источника. Другой маркетплейс, другой репозиторий или каталог вместо
	// GitHub — это НЕ штатный путь, и без прав установка из них отклоняется.
	canonicalMarketplace = "air-plugins"
	canonicalRepo        = "ruhorh66-rgb/air-plugins"
	canonicalPluginKey   = "air-worker@air-plugins"
)

// claudeConfigDir — каталог конфигурации Claude. %CLAUDE_CONFIG_DIR%, если задан, иначе
// %USERPROFILE%\.claude. Источник маркетплейса читается ТОЛЬКО отсюда: заготовка
// extraKnownMarketplaces в settings.json источником не считается (её правка на
// AIR-ENV-002 источник не изменила — маркетплейс остался каталогом).
func claudeConfigDir() string {
	if v := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); v != "" {
		return v
	}
	up := os.Getenv("USERPROFILE")
	if up == "" {
		up = os.Getenv("HOME") // на не-Windows и в тестах
	}
	return filepath.Join(up, ".claude")
}

// --- разбор реестров плагина -------------------------------------------------

type mpSource struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
}

type mpEntry struct {
	Source          mpSource `json:"source"`
	InstallLocation string   `json:"installLocation"`
}

// readMarketplace — запись маркетплейса из known_marketplaces.json. Любая беда чтения
// (файла нет, не разобрался) значит «записи нет»: для решения это то же, что источник
// не GitHub, и различать их незачем.
func readMarketplace(configDir, name string) (mpEntry, bool) {
	var all map[string]mpEntry
	p := filepath.Join(configDir, "plugins", "known_marketplaces.json")
	if err := readJSON(p, &all); err != nil {
		return mpEntry{}, false
	}
	e, ok := all[name]
	return e, ok
}

func marketplaceIsGitHub(e mpEntry) bool {
	return strings.EqualFold(strings.TrimSpace(e.Source.Source), "github") &&
		strings.EqualFold(strings.TrimSpace(e.Source.Repo), canonicalRepo)
}

type installedRec struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
}

// readInstalledPlugin — зарегистрирован ли плагин по installed_plugins.json и какой
// версии. Формат v2: plugins[key] — массив записей; берём первую.
func readInstalledPlugin(configDir, key string) (installedRec, bool) {
	var doc struct {
		Plugins map[string][]installedRec `json:"plugins"`
	}
	p := filepath.Join(configDir, "plugins", "installed_plugins.json")
	if err := readJSON(p, &doc); err != nil {
		return installedRec{}, false
	}
	recs, ok := doc.Plugins[key]
	if !ok || len(recs) == 0 {
		return installedRec{}, false
	}
	return recs[0], true
}

// --- классификация запущенного бинарника ------------------------------------

// binaryInCanonicalCache — лежит ли self в
// <config>\plugins\cache\air-plugins\air-worker\<версия>\bin\. Это ЕДИНСТВЕННОЕ место,
// откуда без прав разрешена установка: туда бинарник кладёт установка плагина из
// GitHub-маркетплейса, и только туда.
func binaryInCanonicalCache(self, configDir string) bool {
	if strings.TrimSpace(self) == "" || strings.TrimSpace(configDir) == "" {
		return false
	}
	binDir := filepath.Dir(self) // .../<версия>/bin
	if !strings.EqualFold(filepath.Base(binDir), "bin") {
		return false
	}
	verDir := filepath.Dir(binDir)    // .../<версия>
	pluginDir := filepath.Dir(verDir) // .../air-worker
	cacheRoot := filepath.Join(configDir, "plugins", "cache", canonicalMarketplace, "air-worker")
	return samePath(pluginDir, cacheRoot)
}

// samePath — совпадают ли два пути. На этой машине каталоги плагина часто связаны
// junction, и сравнение голых строк тогда врёт: реальный путь и путь через junction
// указывают на один каталог на диске, но строками не равны. Ссылки разрешаются с ОБЕИХ
// сторон через resolveLinks; ошибка разрешения (пути нет, или это не ссылка) — не повод
// отказывать сравнению: тогда остаётся прежний результат, по очищенным строкам.
func samePath(a, b string) bool {
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	ra, errA := resolveLinks(a)
	rb, errB := resolveLinks(b)
	if errA != nil || errB != nil {
		return false // прежнее сравнение выше уже дало false
	}
	return strings.EqualFold(filepath.Clean(ra), filepath.Clean(rb))
}

// resolveLinks — путь после разрешения ссылок, включая NTFS junction, компонент за
// компонентом от корня.
//
// filepath.EvalSymlinks здесь НЕ единственный шаг, хотя решение и назвало именно её:
// проверено эмпирически на go1.26.8 windows/amd64 14.09.2026 двумя отдельными
// пробниками, и оба расхождения воспроизводимы, не случайны.
//
//  1. Junction она не разрешает вовсе: os.Lstat не ставит бит os.ModeSymlink для точки
//     соединения (для настоящего симлинка — ставит), а EvalSymlinks проверяет именно
//     этот бит и молча пропускает компонент, оставляя его в строке как обычный
//     каталог. os.Readlink точку соединения читает независимо от этого бита, поэтому
//     здесь путь проверяется Readlink'ом компонент за компонентом от корня — так и
//     ловится junction на ЛЮБОМ уровне, а не только на конечном имени: на этой машине
//     обычно связан junction'ом весь родительский каталог (весь <config>, весь том
//     кэша), а не конечный файл.
//  2. Сама EvalSymlinks попутно приводит короткие имена 8.3 (ADMIN_~1) к длинным — но
//     только когда по пути НЕТ junction; если он есть, короткое имя остаётся как
//     есть. Без второго прохода одна сторона сравнения осталась бы в короткой форме, а
//     другая — в длинной, и строки бы разошлись, хотя каталог на диске один и тот же.
//     Отсюда — EvalSymlinks ЕЩЁ РАЗ, уже после того как junction разрешён руками.
//
// Предел переходов (32) — защита от цикла из ссылок, указывающих друг на друга: такого
// не бывает на практике, но без предела цикл повесил бы вызывающего НАВСЕГДА, а не
// просто дал неверный ответ.
func resolveLinks(p string) (string, error) {
	if _, err := os.Lstat(p); err != nil {
		return "", err // пути нет — дальше проверять нечего
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	vol := filepath.VolumeName(abs)
	parts := strings.Split(abs[len(vol):], string(filepath.Separator))

	cur := vol + string(filepath.Separator)
	hops := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		for {
			target, err := os.Readlink(cur)
			if err != nil {
				break // не ссылка на этом шаге — идём к следующему компоненту
			}
			hops++
			if hops > 32 {
				return "", fmt.Errorf("слишком много переходов по ссылкам: %s", abs)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			cur = filepath.Clean(target)
		}
	}
	// Второй проход: путь уже без junction, и здесь EvalSymlinks нормализует короткие
	// имена 8.3 так же, как сделала бы для пути, где junction никогда не было. Отказ
	// не фатален — cur уже подтверждённо существует (Lstat выше) — просто без этой
	// нормализации остаётся более грубое (но по-прежнему верное) сравнение.
	if final, err := filepath.EvalSymlinks(cur); err == nil {
		cur = final
	}
	return cur, nil
}

func underPluginCache(p string) bool {
	lp := strings.ToLower(filepath.ToSlash(p))
	return strings.Contains(lp, "/plugins/cache/")
}

func describeSource(inCache bool, self string) string {
	switch {
	case inCache:
		return "кэш GitHub-маркетплейса — " + self
	case underPluginCache(self):
		return "кэш другого маркетплейса, не air-plugins/GitHub — " + self
	default:
		return "рабочее дерево или извлечён из тега — " + self
	}
}

func mpSourceShort(mp mpEntry) string {
	switch {
	case mp.Source.Repo != "":
		return mp.Source.Source + " " + mp.Source.Repo
	case mp.Source.Path != "":
		return mp.Source.Source + " " + mp.Source.Path
	default:
		return mp.Source.Source
	}
}

func describeMarketplace(ok bool, mp mpEntry) string {
	if !ok {
		return canonicalMarketplace + " не зарегистрирован"
	}
	return mpSourceShort(mp)
}

// migrateHint — четыре команды перехода машины с каталога на GitHub, одной строкой.
// Их именно четыре, а не две: `marketplace remove` снимает и регистрацию плагина,
// поэтому нужен `plugin install`; `install` ставит плагин выключенным, поэтому нужен
// `plugin enable`. Проверено на AIR-ENV-002 14.09.2026.
func migrateHint() string {
	return "claude plugin marketplace remove air-plugins → " +
		"claude plugin marketplace add ruhorh66-rgb/air-plugins → " +
		"claude plugin install air-worker@air-plugins → " +
		"claude plugin enable air-worker@air-plugins"
}

// --- сравнение версий --------------------------------------------------------

// compareVersions: >0 если a новее b, <0 если старше, 0 если равны. Semver-подобно:
// предвыпуск (1.0.0-beta.1) старше того же ядра без предвыпуска.
func compareVersions(a, b string) int {
	ac, ap := splitPre(a)
	bc, bp := splitPre(b)
	if c := cmpNums(ac, bc); c != 0 {
		return c
	}
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1 // без предвыпуска новее
	case bp == "":
		return -1
	default:
		return strings.Compare(ap, bp)
	}
}

func splitPre(v string) (core, pre string) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 { // отрезаем build metadata
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func cmpNums(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai = atoiPrefix(as[i])
		}
		if i < len(bs) {
			bi = atoiPrefix(bs[i])
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}

func atoiPrefix(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// parseVersionToken — версия из строки `air-worker 0.10.1` → `0.10.1`. Так же читается
// вывод `air-worker version` установленного файла при сверке.
func parseVersionToken(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func verOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(нет)"
	}
	return v
}

// --- пользовательская копия скила -------------------------------------------

// userSkillShadow — пользовательская копия скила в <config>\skills\ с именем woody или
// air-woody. Она ЗАСЛОНЯЕТ плагинный скил: у AIR-ENV-002 при бинарнике 0.10.0
// грузилась ~/.claude/skills/air-woody версии 0.9.4. Это нарушение штатного пути.
func userSkillShadow(configDir string) (string, bool) {
	for _, name := range []string{"woody", "air-woody"} {
		p := filepath.Join(configDir, "skills", name)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// --- SHA-256 -----------------------------------------------------------------

// sha256File — хэш файла для сверки установленного с исходным. Расхождение — код 2,
// а не строка при коде 0: подменённый или недокопированный файл выглядит установленным.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- решение об установке ----------------------------------------------------

type installGuard struct {
	Elevated   bool   // повышены ли права процесса (это ЛПР)
	Self       string // путь запущенного бинарника
	ConfigDir  string // каталог конфигурации Claude
	CodexDir   string // каталог конфигурации Codex; пустой — host не учитывается
	SrcVersion string // версия ставящегося (собственная версия self)
	DstVersion string // версия уже установленного файла назначения, "" если его нет
}

type guardResult struct {
	Allow   bool
	Code    int      // код возврата, если !Allow
	Info    []string // всегда печатается: источник, маркетплейс, при правах — обе версии
	Reasons []string // причины отказа и недостающие штатные команды
}

// decideInstallGuard — сердце шага 66. Без прав установка проходит ТОЛЬКО когда self
// лежит в кэше GitHub-маркетплейса И действующая запись air-plugins — GitHub с нужным
// репозиторием; и только когда это не понижение версии. С правами (ЛПР) разрешено любое,
// но источник и обе версии печатаются, чтобы прямая переустановка не была молчаливой.
func decideInstallGuard(g installGuard) guardResult {
	claudeCache := binaryInCanonicalCache(g.Self, g.ConfigDir)
	var claudeMP mpEntry
	claudeMPOK := false
	if strings.TrimSpace(g.ConfigDir) != "" {
		claudeMP, claudeMPOK = readMarketplace(g.ConfigDir, canonicalMarketplace)
	}
	claudeGitHub := claudeMPOK && marketplaceIsGitHub(claudeMP)

	codexCache := binaryInCanonicalCodexCache(g.Self, g.CodexDir)
	codexMP, codexMPOK, _ := readCodexConfig(g.CodexDir)
	codexGitHub := codexMPOK && codexMarketplaceIsGitHub(codexMP)

	inCache := claudeCache || codexCache
	canonicalSource := (claudeCache && claudeGitHub) || (codexCache && codexGitHub)

	var res guardResult
	res.Info = append(res.Info, "Источник   : "+describeSource(inCache, g.Self))
	switch {
	case codexCache:
		res.Info = append(res.Info, "Маркетплейс: Codex "+describeCodexMarketplace(codexMPOK, codexMP))
	case claudeCache:
		res.Info = append(res.Info, "Маркетплейс: Claude "+describeMarketplace(claudeMPOK, claudeMP))
	case claudeMPOK:
		res.Info = append(res.Info, "Маркетплейс: Claude "+describeMarketplace(true, claudeMP))
	case codexMPOK:
		res.Info = append(res.Info, "Маркетплейс: Codex "+describeCodexMarketplace(true, codexMP))
	default:
		res.Info = append(res.Info, "Маркетплейс: air-plugins не зарегистрирован")
	}

	if g.Elevated {
		res.Allow = true
		res.Info = append(res.Info, fmt.Sprintf(
			"Права      : повышены (ЛПР) — прямая переустановка разрешена; ставится %s поверх %s",
			verOrDash(g.SrcVersion), verOrDash(g.DstVersion)))
		return res
	}

	if !canonicalSource {
		res.Code = 2
		switch {
		case !inCache && !underPluginCache(g.Self):
			res.Reasons = append(res.Reasons,
				"ОТКАЗ: бинарник не из кэша плагина — рабочее дерево или извлечён из тега; установка только из GitHub-маркетплейса.")
		case !inCache:
			res.Reasons = append(res.Reasons,
				"ОТКАЗ: бинарник из кэша другого маркетплейса, не air-plugins/GitHub.")
		case codexCache:
			res.Reasons = append(res.Reasons,
				"ОТКАЗ: Codex marketplace air-plugins не GitHub ("+codexSourceShort(codexMP)+").")
		default:
			res.Reasons = append(res.Reasons,
				"ОТКАЗ: Claude marketplace air-plugins не GitHub ("+mpSourceShort(claudeMP)+") — правка settings.json источник не меняет.")
		}
		if codexCache {
			res.Reasons = append(res.Reasons, "Штатно перевести Codex на GitHub: "+codexMigrateHint())
		} else {
			res.Reasons = append(res.Reasons, "Штатно перевести Claude на GitHub: "+migrateHint())
		}
		return res
	}

	if g.DstVersion != "" && compareVersions(g.DstVersion, g.SrcVersion) > 0 {
		res.Code = 2
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"ОТКАЗ: установлена версия %s новее ставящейся %s — понижение без повышенных прав запрещено.",
			g.DstVersion, g.SrcVersion))
		return res
	}

	res.Allow = true
	return res
}

// installedBinarySource — источник УСТАНОВЛЕННОГО бинарника (installedPath — тот же файл,
// что dstCLI в cmdInstall): сверка его SHA-256 с канонической копией в кэше
// GitHub-маркетплейса — bin\air-worker.exe внутри installPath записи air-worker@air-plugins
// из installed_plugins.json. Пустая причина значит «совпало»; иначе — причина без домысливания.
func installedBinarySource(installedPath string, rec installedRec) (line, reason string) {
	cachePath := filepath.Join(rec.InstallPath, "bin", "air-worker.exe")
	hi, ei := sha256File(installedPath)
	if ei != nil {
		return "", fmt.Sprintf("установленный бинарник не прочитан для сверки SHA-256: %s — %v", installedPath, ei)
	}
	hc, ec := sha256File(cachePath)
	if ec != nil {
		return "", fmt.Sprintf("файл кэша GitHub-маркетплейса не прочитан для сверки: %s — %v", cachePath, ec)
	}
	if !strings.EqualFold(hi, hc) {
		return "", fmt.Sprintf("SHA-256 установленного бинарника не совпал с кэшем GitHub-маркетплейса (%s)", cachePath)
	}
	return "кэш GitHub-маркетплейса, версия " + rec.Version + ", SHA-256 совпадает", ""
}

// installSourceStatusLines — строки `install -status` про источник: маркетплейс,
// регистрация плагина, источник УСТАНОВЛЕННОГО бинарника (installedPath — судится он, а
// не self), источник ЗАПУЩЕННОГО бинарника отдельной информационной строкой, пользовательская
// копия скила. Нарушения возвращаются отдельным списком — каждое с недостающей штатной
// командой или причиной расхождения.
//
// До этой правки функция судила источник ЗАПУЩЕННОГО бинарника (self). Но `install
// -status` обычно вызывают командой `air-worker install -status` из PATH — то есть самой
// УСТАНОВЛЕННОЙ копией, — и она получала «НАРУШЕНИЕ: запущенный бинарник не из кэша
// GitHub-маркетплейса» всегда, даже когда установка совершенно штатна: self и есть
// installedPath, а не кэш плагина, и в кэше ему взяться неоткуда.
func installSourceStatusLines(configDir, installedPath, self string) (lines, violations []string) {
	return installSourceStatusLinesForHosts(configDir, "", installedPath, self)
}

func installSourceStatusLinesForHosts(claudeDir, codexDir, installedPath, self string) (lines, violations []string) {
	var claudeMP mpEntry
	claudeMPOK := false
	if strings.TrimSpace(claudeDir) != "" {
		claudeMP, claudeMPOK = readMarketplace(claudeDir, canonicalMarketplace)
	}
	claudeGitHub := claudeMPOK && marketplaceIsGitHub(claudeMP)
	codexMP, codexMPOK, _ := readCodexConfig(codexDir)
	codexGitHub := codexMPOK && codexMarketplaceIsGitHub(codexMP)

	if claudeMPOK {
		lines = append(lines, "Маркетплейс Claude: "+describeMarketplace(true, claudeMP))
	}
	if codexMPOK {
		lines = append(lines, "Маркетплейс Codex: "+describeCodexMarketplace(true, codexMP))
	}
	if !claudeMPOK && !codexMPOK {
		lines = append(lines, "Маркетплейс: air-plugins не зарегистрирован")
	}

	var claudeRec installedRec
	claudeReg := false
	if strings.TrimSpace(claudeDir) != "" {
		claudeRec, claudeReg = readInstalledPlugin(claudeDir, canonicalPluginKey)
	}
	codexRec, codexReg := readCodexInstalledPlugin(codexDir)
	if claudeReg {
		lines = append(lines, "Плагин Claude: зарегистрирован "+canonicalPluginKey+" версии "+claudeRec.Version)
	}
	if codexReg {
		lines = append(lines, "Плагин Codex : зарегистрирован "+canonicalPluginKey+" версии "+codexRec.Version)
	}
	if !claudeReg && !codexReg {
		lines = append(lines, "Плагин     : НЕ зарегистрирован "+canonicalPluginKey)
	}

	sourceMatched := false
	var sourceReasons []string
	if claudeReg && claudeGitHub {
		if line, why := installedBinarySource(installedPath, claudeRec); why == "" {
			lines = append(lines, "Источник   : Claude "+line)
			sourceMatched = true
		} else {
			sourceReasons = append(sourceReasons, why)
		}
	}
	if !sourceMatched && codexReg && codexGitHub {
		if line, why := installedBinarySource(installedPath, codexRec); why == "" {
			lines = append(lines, "Источник   : Codex "+line)
			sourceMatched = true
		} else {
			sourceReasons = append(sourceReasons, why)
		}
	}
	if !sourceMatched && len(sourceReasons) > 0 {
		for _, why := range sourceReasons {
			violations = append(violations, "НАРУШЕНИЕ: "+why)
		}
	}

	inCacheRunning := binaryInCanonicalCache(self, claudeDir) || binaryInCanonicalCodexCache(self, codexDir)
	lines = append(lines, "Запущен    : "+describeSource(inCacheRunning, self))

	if claudeDir != "" {
		if p, shadow := userSkillShadow(claudeDir); shadow {
			lines = append(lines, "Скил       : пользовательская копия "+p+" заслоняет плагинный")
			violations = append(violations,
				"НАРУШЕНИЕ: пользовательская копия скила заслоняет плагинный — снять с резервной копией: "+p)
		}
	}

	if claudeMPOK && !claudeGitHub {
		violations = append(violations,
			"НАРУШЕНИЕ: Claude marketplace air-plugins не GitHub ("+mpSourceShort(claudeMP)+"). Перевести штатно: "+migrateHint())
	}
	if codexMPOK && !codexGitHub {
		violations = append(violations,
			"НАРУШЕНИЕ: Codex marketplace air-plugins не GitHub ("+codexSourceShort(codexMP)+"). Перевести штатно: "+codexMigrateHint())
	}
	if !claudeMPOK && !codexMPOK {
		violations = append(violations,
			"НАРУШЕНИЕ: marketplace air-plugins не зарегистрирован ни в Claude, ни в Codex. Claude: "+migrateHint()+"; Codex: "+codexMigrateHint())
	}
	if !claudeReg && !codexReg {
		violations = append(violations,
			"НАРУШЕНИЕ: плагин не зарегистрирован ни в Claude, ни в Codex. Штатно установить через GitHub marketplace.")
	}
	if (claudeReg || codexReg) && !sourceMatched && len(sourceReasons) == 0 {
		violations = append(violations,
			"НАРУШЕНИЕ: зарегистрированный плагин не имеет подтверждённого GitHub source/cache для сверки установленного бинарника.")
	}
	return lines, violations
}
