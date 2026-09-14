//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// ПОДЪЁМ ЗНАЧКА ПО ЗАГРУЗКЕ ПЛАГИНА, А НЕ ПО ВХОДУ В СИСТЕМУ.
//
// Указание ЛПР 14.09.2026: «хочу, чтобы при загрузке плагина, когда я даю команду
// загрузить плагин air-worker, дальше поднимался значок и запускался продукт».
//
// Это ТРЕТЬЯ формулировка подряд, и разница между ними существенна:
//
//	автозапуск при входе  — машина решает за человека. ОТВЕРГНУТО ЛПР.
//	запуск командой       — человек каждый раз делает лишнее движение.
//	подъём при загрузке   — продукт живёт ровно тогда, когда им пользуются.
//
// Последнее и есть верное: значок привязан к работе с плагином, а не к включению
// машины, и ничего в реестре не остаётся.
//
// ИДЕМПОТЕНТНО, И ЭТО ГЛАВНОЕ ТРЕБОВАНИЕ. На машине работают несколько сессий
// одновременно (прямая вводная ЛПР того же дня), каждая при загрузке зовёт эту команду,
// и все они обязаны получить ОДИН значок. Гонка разрешается не здесь, а в самом значке:
// он держит именованный мьютекс и второй экземпляр выходит сразу. Проверка ниже — лишь
// способ не плодить процессы впустую; даже если она промахнётся, вреда не будет.
//
// ПОЧЕМУ МЬЮТЕКС, А НЕ ПОИСК ОКНА. Замер 14.09.2026: оболочка, в которой исполняются
// хуки, НЕ ВИДИТ ОКОН интерактивного рабочего стола — она не нашла даже заведомо живое
// окно значка. Поиск окна здесь ответил бы «не запущен» всегда, и каждая загрузка
// плодила бы процесс. Мьютекс ядра виден из любого процесса сеанса.

const trayMutexName = `Local\air-worker-tray`

// trayBinary — значок рядом с CLI. Тот же довод, что у значка про CLI: в PATH может
// оказаться другая версия, и подниматься будет не тот продукт, что зовут.
func trayBinary() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(self), "air-worker-tray.exe")
}

// startTrayDetached запускает значок отцеплённым. Флаги те же, что в установке, и по
// той же причине: вызывающий (хук харнесса) ждёт дерево процессов, а значок живёт
// вечно — без отцепления загрузка плагина подвисала бы навсегда.
func startTrayDetached(path string) error {
	const (
		createNewProcessGroup  = 0x00000200
		detachedProcess        = 0x00000008
		createBreakawayFromJob = 0x01000000
	)
	try := func(flags uint32) error {
		c := exec.Command(path)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := c.Start(); err != nil {
			return err
		}
		_ = c.Process.Release()
		return nil
	}
	if err := try(createNewProcessGroup | detachedProcess | createBreakawayFromJob); err == nil {
		return nil
	}
	// Выход из объекта задания разрешён не всегда. Отказ по нему — не повод не поднять
	// значок: повторяем без этого флага.
	return try(createNewProcessGroup | detachedProcess)
}

func cmdTray(argv []string) int {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	ensure := fs.Bool("ensure", false, "поднять значок, если он ещё не поднят (идемпотентно)")
	stop := fs.Bool("stop", false, "попросить значок выйти")
	status := fs.Bool("status", false, "ответить, поднят ли значок")
	quiet := fs.Bool("quiet", false, "молча, только код возврата")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	say := func(f string, a ...any) {
		if !*quiet {
			fmt.Printf(f+lineEnding, a...)
		}
	}

	switch {
	case *status:
		if lockHeld(trayMutexName) {
			say("значок поднят")
			return 0
		}
		say("значок не поднят")
		return 1

	case *stop:
		asked, gone := stopTray()
		switch {
		case !asked:
			say("значок и так не поднят")
		case gone:
			say("значок остановлен")
		default:
			say("значок не ответил на запрос выхода за пять секунд")
			return 1
		}
		return 0

	case *ensure:
		if lockHeld(trayMutexName) {
			// МОЛЧАНИЕ ЗДЕСЬ НАМЕРЕННОЕ. Команду зовёт хук при каждой загрузке плагина;
			// строка «уже поднят» на каждый старт сессии превратила бы полезное
			// сообщение в шум, который перестают читать.
			return 0
		}
		bin := trayBinary()
		if bin == "" {
			say("НЕЧЕМ ПОДНЯТЬ: не разобран собственный путь")
			return 2
		}
		if _, err := os.Stat(bin); err != nil {
			say("НЕЧЕМ ПОДНЯТЬ: рядом нет %s — собери его или поставь продукт заново", bin)
			return 2
		}
		if err := startTrayDetached(bin); err != nil {
			say("значок не поднялся: %v", err)
			return 1
		}
		say("значок поднят")
		return 0
	}

	fmt.Fprint(os.Stderr, "укажи -ensure, -stop или -status"+lineEnding)
	return 2
}
