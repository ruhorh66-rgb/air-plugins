//go:build windows

package main

import (
	"encoding/binary"
	"strings"
	"syscall"
	"testing"
)

func ptr(v int) *int { return &v }

// Вид значка — единственное, что механизм говорит без слов, и перепутанные состояния
// здесь стоят дороже всего: человек смотрит на трей мельком и второй раз не проверяет.
func TestShapeAndColor(t *testing.T) {
	cases := []struct {
		name  string
		in    []productState
		shape iconShape
		color rgba
	}{
		{
			// «Продуктов нет» — это НЕ «всё хорошо». Зелёный кружок здесь означал бы,
			// что механизм доволен там, где он вообще ничего не мерил.
			name: "продуктов не объявлено — серое кольцо",
			in:   nil, shape: shapeRing, color: colorUnknown,
		},
		{
			name:  "нечем измерить — серое кольцо, а не зелёный круг",
			in:    []productState{{Name: "a", Distance: nil}},
			shape: shapeRing, color: colorUnknown,
		},
		{
			name:  "ошибка замера — тоже серое кольцо",
			in:    []productState{{Name: "a", Err: "drift не ответил"}},
			shape: shapeRing, color: colorUnknown,
		},
		{
			name:  "расстояние ноль у всех — зелёный круг",
			in:    []productState{{Name: "a", Distance: ptr(0), Verdict: "ALLOW"}},
			shape: shapeDisc, color: colorOK,
		},
		{
			name:  "работа идёт — жёлтый круг",
			in:    []productState{{Name: "a", Distance: ptr(7), Verdict: "ALLOW"}},
			shape: shapeDisc, color: colorWork,
		},
		{
			name:  "эскалация — красный с вырезом",
			in:    []productState{{Name: "a", Distance: ptr(7), Verdict: "ESCALATE"}},
			shape: shapeNotch, color: colorAttn,
		},
		{
			name:  "ждёт ЛПР — красный с вырезом",
			in:    []productState{{Name: "a", Distance: ptr(0), Verdict: "ЖДЁТ ЛПР"}},
			shape: shapeNotch, color: colorAttn,
		},
		{
			// СТАРШИНСТВО. Тревога у одного продукта не гасится благополучием
			// остальных: значок обязан показывать худшее, иначе он успокаивает.
			name: "тревога старше благополучия",
			in: []productState{
				{Name: "a", Distance: ptr(0), Verdict: "ALLOW"},
				{Name: "b", Distance: ptr(3), Verdict: "THROTTLE"},
			},
			shape: shapeNotch, color: colorAttn,
		},
		{
			// А работа — старше «нечем измерить»: делать есть что, и это важнее того,
			// что рядом чего-то не измерили.
			name: "работа старше неизмеримого",
			in: []productState{
				{Name: "a", Distance: nil},
				{Name: "b", Distance: ptr(3), Verdict: "ALLOW"},
			},
			shape: shapeDisc, color: colorWork,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shape, color := shapeAndColor(c.in)
			if shape != c.shape || color != c.color {
				t.Fatalf("вид значка разошёлся: получено (%v, %v), ожидалось (%v, %v)",
					shape, color, c.shape, c.color)
			}
		})
	}
}

// Образ значка отдаётся в ОС напрямую. Ошибка в заголовке не вызовет отказа —
// CreateIconFromResourceEx вернёт ноль либо мусорную картинку, и в трее появится
// пустое место, неотличимое от «не запущен».
func TestRenderIconImage(t *testing.T) {
	img := renderIconImage(colorOK, shapeDisc)
	const want = 40 + iconSize*iconSize*4 + iconSize*4
	if len(img) != want {
		t.Fatalf("длина образа %d, ожидалась %d", len(img), want)
	}
	if got := binary.LittleEndian.Uint32(img[0:]); got != 40 {
		t.Errorf("biSize %d, ожидался 40", got)
	}
	if got := binary.LittleEndian.Uint32(img[4:]); got != iconSize {
		t.Errorf("ширина %d, ожидалась %d", got, iconSize)
	}
	// ВЫСОТА УДВОЕНА — так устроен образ значка: цвет плюс маска. Написать сюда
	// обычную высоту значит отдать ОС ровно половину картинки.
	if got := binary.LittleEndian.Uint32(img[8:]); got != iconSize*2 {
		t.Errorf("высота %d, ожидалась удвоенная %d", got, iconSize*2)
	}
	if got := binary.LittleEndian.Uint16(img[14:]); got != 32 {
		t.Errorf("бит на точку %d, ожидалось 32", got)
	}

	// Три формы обязаны РАЗЛИЧАТЬСЯ. Значок, одинаковый во всех состояниях, проходит
	// любую проверку размеров и не сообщает ничего.
	disc := renderIconImage(colorOK, shapeDisc)
	notch := renderIconImage(colorOK, shapeNotch)
	ring := renderIconImage(colorOK, shapeRing)
	if string(disc) == string(notch) || string(disc) == string(ring) || string(notch) == string(ring) {
		t.Error("формы значка неразличимы — состояние видно только цветом")
	}
}

// Подсказка уходит в массив фиксированной длины, который ОС читает до нуля.
// Массив без нуля — чтение за пределами, то есть мусор в подсказке или падение
// проводника, а не наша ошибка в нашем процессе.
func TestCopyTipAlwaysTerminates(t *testing.T) {
	var dst [128]uint16
	long := strings.Repeat("длинная подсказка ", 40)
	copyTip(&dst, long)
	if dst[len(dst)-1] != 0 {
		t.Fatal("последняя ячейка не ноль: ОС прочитает за пределы массива")
	}
	got := syscall.UTF16ToString(dst[:])
	if len(got) == 0 {
		t.Fatal("подсказка потерялась целиком")
	}
	if !strings.HasPrefix(long, got) {
		t.Fatalf("подсказка не просто обрезана, а искажена: %q", got)
	}

	// Короткая строка обязана дойти целиком — обрезка не должна срабатывать всегда.
	var short [128]uint16
	copyTip(&short, "коротко")
	if syscall.UTF16ToString(short[:]) != "коротко" {
		t.Fatalf("короткая подсказка искажена: %q", syscall.UTF16ToString(short[:]))
	}
}

// Подсказка называет КАЖДЫЙ объявленный продукт числом. Сводка «всё хорошо» без чисел
// вернула бы нас к самоотчёту, от которого механизм и уходит.
func TestTooltipNamesEveryProductWithNumbers(t *testing.T) {
	tip := tooltip([]productState{
		{Name: "asw", Distance: ptr(24), Verdict: "ALLOW"},
		{Name: "air-worker", Distance: nil},
	})
	if !strings.Contains(tip, "asw: 24") {
		t.Errorf("в подсказке нет числа продукта asw: %q", tip)
	}
	if !strings.Contains(tip, "нечем измерить") {
		t.Errorf("неизмеренный продукт не назван: %q", tip)
	}
	if strings.Contains(tip, "air-worker: 0") {
		t.Errorf("неизмеренное выдано за ноль: %q", tip)
	}
}
