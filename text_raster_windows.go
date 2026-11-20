package main

import (
	"image"
	"math"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/gogpu/ui/widget"
	"golang.org/x/sys/windows"
)

var (
	gdiText            = windows.NewLazySystemDLL("gdi32.dll")
	createMemoryDC     = gdiText.NewProc("CreateCompatibleDC")
	deleteMemoryDC     = gdiText.NewProc("DeleteDC")
	createTextFont     = gdiText.NewProc("CreateFontW")
	getTextFace        = gdiText.NewProc("GetTextFaceW")
	createTextBitmap   = gdiText.NewProc("CreateBitmap")
	createTextDIB      = gdiText.NewProc("CreateDIBSection")
	selectTextObject   = gdiText.NewProc("SelectObject")
	deleteTextObject   = gdiText.NewProc("DeleteObject")
	getTextExtent      = gdiText.NewProc("GetTextExtentPoint32W")
	getTextExtents     = gdiText.NewProc("GetTextExtentExPointW")
	setTextColor       = gdiText.NewProc("SetTextColor")
	setTextBackground  = gdiText.NewProc("SetBkColor")
	fillTextBackground = gdiText.NewProc("PatBlt")
	textOut            = gdiText.NewProc("TextOutW")
	flushText          = gdiText.NewProc("GdiFlush")
)

type windowsTextRasterizer struct {
	mu    sync.Mutex
	lines map[windowsTextKey]windowsTextLine
	bytes int
}

type windowsTextKey struct {
	text, family string
	pixels       int
	bold, italic bool
	color        widget.Color // zero when measuring
}

type windowsTextLine struct {
	bitmap  widget.TextBitmap
	metrics widget.RasterTextMetrics
}

func newPlatformTextRasterizer() widget.TextRasterizer {
	return &windowsTextRasterizer{lines: make(map[windowsTextKey]windowsTextLine)}
}

func (r *windowsTextRasterizer) Rasterize(s string, style widget.TextStyle, scale float32) (widget.TextBitmap, bool) {
	if style.Color.A == 0 {
		return widget.TextBitmap{}, false
	}
	line, ok := r.line(s, style, scale)
	return line.bitmap, ok
}

func (r *windowsTextRasterizer) Measure(s string, style widget.TextStyle, scale float32) (widget.RasterTextMetrics, bool) {
	style.Color = widget.Color{}
	line, ok := r.line(s, style, scale)
	return line.metrics, ok
}

func (r *windowsTextRasterizer) line(s string, style widget.TextStyle, scale float32) (windowsTextLine, bool) {
	pixels := int(math.Round(float64(style.FontSize * scale)))
	if s == "" || len(s) > 4096 || pixels < 1 || pixels > 512 {
		return windowsTextLine{}, false
	}
	key := windowsTextKey{s, style.FontFamily, pixels, style.Bold, style.Italic, style.Color}
	r.mu.Lock()
	defer r.mu.Unlock()
	if line, ok := r.lines[key]; ok {
		return line, true
	}
	line, ok := rasterizeWindowsLine(key)
	if !ok {
		return windowsTextLine{}, false
	}

	size := len(key.text) + len(key.family) + len(line.metrics.CaretOffsets)*8
	if line.bitmap.Image != nil {
		size += len(line.bitmap.Image.Pix)
	}
	const budget = 4 << 20
	if size <= budget {
		if len(r.lines) >= 512 || r.bytes+size > budget {
			clear(r.lines)
			r.bytes = 0
		}
		r.lines[key] = line
		r.bytes += size
	}
	return line, true
}

// A top-down, 32-bit BI_RGB DIB has no palette.
type textBitmapInfo struct {
	size             uint32
	width, height    int32
	planes, bits     uint16
	compression      uint32
	imageSize        uint32
	xPixels, yPixels int32
	used, important  uint32
}

func rasterizeWindowsLine(key windowsTextKey) (windowsTextLine, bool) {
	chars, err := windows.UTF16FromString(key.text)
	if err != nil {
		return windowsTextLine{}, false
	}
	// A memory DC created without a source DC belongs to its creating thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dc, _, _ := createMemoryDC.Call(0)
	if dc == 0 {
		return windowsTextLine{}, false
	}
	defer deleteMemoryDC.Call(dc)

	// A color surface is needed even for measurement. The default DC surface
	// is monochrome, which can change the advances of ClearType text.
	sample, _, _ := createTextBitmap.Call(1, 1, 1, 32, 0)
	if sample == 0 {
		return windowsTextLine{}, false
	}
	defer deleteTextObject.Call(sample)
	previous, ok := selectObject(dc, sample)
	if !ok {
		return windowsTextLine{}, false
	}
	defer selectTextObject.Call(dc, previous)

	font, oldFont, ok := selectWindowsFont(dc, key)
	if !ok {
		return windowsTextLine{}, false
	}
	defer deleteTextObject.Call(font)
	defer selectTextObject.Call(dc, oldFont)

	var extent struct{ width, height int32 }
	charCount := len(chars) - 1
	if key.color.A == 0 {
		// Read the prefix advances from the same full line, in UTF-16 units.
		dx := make([]int32, charCount)
		var fit int32
		result, _, _ := getTextExtents.Call(dc, uintptr(unsafe.Pointer(&chars[0])), uintptr(charCount),
			math.MaxInt32, uintptr(unsafe.Pointer(&fit)), uintptr(unsafe.Pointer(&dx[0])), uintptr(unsafe.Pointer(&extent)))
		if result == 0 || extent.height <= 0 || extent.width < 0 {
			return windowsTextLine{}, false
		}
		carets := make([]int, 1, utf8.RuneCountInString(key.text)+1)
		i := 0
		for _, r := range key.text {
			i++
			if r > 0xffff {
				i++
			}
			carets = append(carets, int(dx[i-1]))
		}
		carets[len(carets)-1] = int(extent.width)
		return windowsTextLine{metrics: widget.RasterTextMetrics{Advance: int(extent.width), CaretOffsets: carets}}, true
	}

	result, _, _ := getTextExtent.Call(dc, uintptr(unsafe.Pointer(&chars[0])), uintptr(charCount), uintptr(unsafe.Pointer(&extent)))
	if result == 0 || extent.height <= 0 || extent.width < 0 {
		return windowsTextLine{}, false
	}
	// Leave room for ink overhangs without including them in the text advance.
	padding := key.pixels
	width, height := int(extent.width)+2*padding, int(extent.height)
	if int64(width)*int64(height) > 1<<20 {
		return windowsTextLine{}, false
	}
	img, ok := drawWindowsLine(dc, chars, key.color, width, height, padding)
	if !ok {
		return windowsTextLine{}, false
	}
	return windowsTextLine{bitmap: widget.TextBitmap{Image: img, Advance: int(extent.width), InkPadding: padding}}, true
}

func selectWindowsFont(dc uintptr, key windowsTextKey) (font, previous uintptr, ok bool) {
	families := []string{key.family}
	if key.family == "" {
		families = []string{"Segoe UI", "Arial"}
	}
	weight, italic := uintptr(400), uintptr(0)
	if key.bold {
		weight = 700
	}
	if key.italic {
		italic = 1
	}
	for _, family := range families {
		name, err := windows.UTF16PtrFromString(family)
		if err != nil {
			continue
		}
		const clearTypeNatural = 6
		font, _, _ = createTextFont.Call(uintptr(-key.pixels), 0, 0, 0, weight,
			italic, 0, 0, 1, 0, 0, clearTypeNatural, 0, uintptr(unsafe.Pointer(name)))
		if font == 0 {
			continue
		}
		previous, ok = selectObject(dc, font)
		if ok {
			var face [32]uint16
			length, _, _ := getTextFace.Call(dc, uintptr(len(face)), uintptr(unsafe.Pointer(&face[0])))
			if length != 0 && strings.EqualFold(windows.UTF16ToString(face[:]), family) {
				return font, previous, true
			}
			selectTextObject.Call(dc, previous)
		}
		deleteTextObject.Call(font)
	}
	return 0, 0, false
}

func selectObject(dc, object uintptr) (uintptr, bool) {
	old, _, _ := selectTextObject.Call(dc, object)
	return old, old != 0 && old != ^uintptr(0)
}

func drawWindowsLine(dc uintptr, chars []uint16, color widget.Color, width, height, padding int) (*image.RGBA, bool) {
	header := textBitmapInfo{width: int32(width), height: -int32(height), planes: 1, bits: 32}
	header.size = uint32(unsafe.Sizeof(header))
	var bits unsafe.Pointer
	bitmap, _, _ := createTextDIB.Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 {
		return nil, false
	}
	defer deleteTextObject.Call(bitmap)
	if bits == nil {
		return nil, false
	}
	previous, ok := selectObject(dc, bitmap)
	if !ok {
		return nil, false
	}
	defer selectTextObject.Call(dc, previous)
	const whiteness = 0x00ff0062
	result, _, _ := fillTextBackground.Call(dc, 0, 0, uintptr(width), uintptr(height), whiteness)
	if result == 0 {
		return nil, false
	}

	// As in Toga, sample dark ink on white and combine the ClearType subpixels
	// into a neutral alpha mask so the line composites on either UI theme.
	setTextBackground.Call(dc, 0xffffff)
	setTextColor.Call(dc, 0)
	result, _, _ = textOut.Call(dc, uintptr(padding), 0, uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1))
	if result == 0 {
		return nil, false
	}
	flushText.Call()
	mask := unsafe.Slice((*byte)(bits), 4*width*height)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(mask); i += 4 {
		coverage := 255 - float32(int(mask[i])+int(mask[i+1])+int(mask[i+2]))/3
		alpha := coverage * color.A
		img.Pix[i] = byte(color.R*alpha + 0.5)
		img.Pix[i+1] = byte(color.G*alpha + 0.5)
		img.Pix[i+2] = byte(color.B*alpha + 0.5)
		img.Pix[i+3] = byte(alpha + 0.5)
	}
	return img, true
}
