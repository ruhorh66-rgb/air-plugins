//go:build windows

package main

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

// ЗНАЧОК РИСУЕТСЯ В ПАМЯТИ, А НЕ ЛЕЖИТ ФАЙЛОМ РЯДОМ.
//
// Причина практическая: продукт ставится КОПИРОВАНИЕМ ОДНОГО ФАЙЛА и потому не требует
// повышения прав. Файл значка рядом — это второй файл, который может не доехать, и
// тогда в трее появится пустое место либо системная заглушка, неотличимая от чужого
// приложения. Приём взят у ASW (internal/trayicon), где он уже отработан.
//
// ТРИ СОСТОЯНИЯ РАЗЛИЧИМЫ НЕ ТОЛЬКО ЦВЕТОМ. Зелёный круг, красный круг С ВЫРЕЗОМ и
// серое КОЛЬЦО: форма разная у каждого. Значок, различимый только цветом, для части
// людей не различим вовсе, а трей — это единственное место, где механизм сообщает о
// себе без слов.

const iconSize = 16

type rgba struct{ r, g, b, a byte }

var (
	colorOK      = rgba{0x2f, 0xa8, 0x4b, 0xff} // цель достигнута, сноса нет
	colorAttn    = rgba{0xd1, 0x3b, 0x2d, 0xff} // застой, эскалация или «ждёт ЛПР»
	colorWork    = rgba{0xd8, 0x99, 0x1e, 0xff} // работа идёт, расстояние не ноль
	colorUnknown = rgba{0x8a, 0x8a, 0x8a, 0xff} // мерить нечего: продукт не объявлен
)

type iconShape int

const (
	shapeDisc  iconShape = iota // сплошной круг
	shapeNotch                  // круг с вырезом справа
	shapeRing                   // кольцо
)

// renderIconImage собирает то, что CreateIconFromResourceEx называет «образом значка»:
// BITMAPINFOHEADER с УДВОЕННОЙ высотой, затем цвет снизу вверх, затем маска.
// Заголовка ICO-файла здесь быть не должно — функция ждёт именно образ.
func renderIconImage(c rgba, shape iconShape) []byte {
	const w, h = iconSize, iconSize
	buf := make([]byte, 0, 40+w*h*4+h*4)

	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)             // biSize
	binary.LittleEndian.PutUint32(hdr[4:], uint32(w))      // biWidth
	binary.LittleEndian.PutUint32(hdr[8:], uint32(h*2))    // biHeight: цвет + маска
	binary.LittleEndian.PutUint16(hdr[12:], 1)             // biPlanes
	binary.LittleEndian.PutUint16(hdr[14:], 32)            // biBitCount
	binary.LittleEndian.PutUint32(hdr[16:], 0)             // BI_RGB
	binary.LittleEndian.PutUint32(hdr[20:], uint32(w*h*4)) // biSizeImage
	buf = append(buf, hdr...)

	cx, cy := float64(w-1)/2, float64(h-1)/2
	outer := 7.0
	inner := 4.2

	// Цветные точки идут СНИЗУ ВВЕРХ — так устроен DIB, и перепутать порядок значит
	// получить перевёрнутый значок, чего в трее 16x16 почти не видно у круга, но
	// сразу видно у выреза.
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d2 := dx*dx + dy*dy
			on := false
			switch shape {
			case shapeDisc:
				on = d2 <= outer*outer
			case shapeNotch:
				on = d2 <= outer*outer && !(dx > 1.5 && dy > -2.0 && dy < 2.0)
			case shapeRing:
				on = d2 <= outer*outer && d2 >= inner*inner
			}
			if on {
				buf = append(buf, c.b, c.g, c.r, c.a) // BGRA
			} else {
				buf = append(buf, 0, 0, 0, 0)
			}
		}
	}

	// Маска прозрачности не нужна при 32 битах с альфой, но структура её требует:
	// нули означают «брать альфу из цвета».
	buf = append(buf, make([]byte, h*4)...)
	return buf
}

// createIcon отдаёт дескриптор значка. Ноль означает «ОС отказала» — вызывающий обязан
// это различать: значок-ноль Shell_NotifyIcon примет, и в трее появится пустое место.
func createIcon(c rgba, shape iconShape) syscall.Handle {
	img := renderIconImage(c, shape)
	h, _, _ := procCreateIconFromResEx.Call(
		uintptr(unsafe.Pointer(&img[0])),
		uintptr(len(img)),
		1,          // fIcon: значок, не курсор
		0x00030000, // версия формата, которую ждёт функция
		0, 0,       // размеры: брать из образа
		0, // LR_DEFAULTCOLOR
	)
	return syscall.Handle(h)
}
