package main

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/gogpu/gg/scene"
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
)

func TestWindowsTextRasterizerScalesAndKeepsGlyphs(t *testing.T) {
	rasterizer := newPlatformTextRasterizer()
	for _, style := range []widget.TextStyle{
		{FontSize: 13, Color: widget.ColorBlack},
		{FontSize: 14, Color: widget.ColorWhite, Bold: true},
		{FontFamily: "Consolas", FontSize: 13, Color: widget.ColorWhite},
	} {
		const label = "Обновить ветку из Git"
		previous := 0
		for _, scale := range []float32{1, 1.25, 1.5, 2} {
			line, ok := rasterizer.Rasterize(label, style, scale)
			if !ok && style.FontFamily != "" && previous == 0 {
				// Some Windows installations do not include Consolas.
				break
			}
			if !ok || line.Image == nil || line.Advance <= previous {
				t.Fatalf("missing or incorrectly scaled line (%+v, %v×)", style, scale)
			}
			previous = line.Advance
			metrics, ok := rasterizer.Measure(label, style, scale)
			if !ok || metrics.Advance != line.Advance || len(metrics.CaretOffsets) != utf8.RuneCountInString(label)+1 ||
				metrics.CaretOffsets[0] != 0 || metrics.CaretOffsets[len(metrics.CaretOffsets)-1] != line.Advance {
				t.Fatalf("caret and advance do not match the rasterized line at %v×: %+v", scale, metrics)
			}
			for i := 1; i < len(metrics.CaretOffsets); i++ {
				if metrics.CaretOffsets[i] < metrics.CaretOffsets[i-1] {
					t.Fatalf("caret offsets are out of order at %v×: %v", scale, metrics.CaretOffsets)
				}
			}
			reference, ok := rasterizer.Rasterize(label+"        ", style, scale)
			if !ok || reference.Image == nil || reference.Image.Rect.Dy() != line.Image.Rect.Dy() || reference.Image.Stride < line.Image.Stride {
				t.Fatalf("could not compare line to a wider bitmap at %v×", scale)
			}
			antialiased := false
			for y := range line.Image.Rect.Dy() {
				row := line.Image.Pix[y*line.Image.Stride : (y+1)*line.Image.Stride]
				ref := reference.Image.Pix[y*reference.Image.Stride : (y+1)*reference.Image.Stride]
				if !bytes.Equal(row, ref[:len(row)]) || len(bytes.Trim(ref[len(row):], "\x00")) != 0 {
					t.Fatalf("line clipped its final glyph at %v× on row %d", scale, y)
				}
				for i := 3; i < len(row); i += 4 {
					if row[i] > 0 && row[i] < 255 {
						antialiased = true
					}
				}
			}
			if !antialiased {
				t.Errorf("no antialiasing at %v× (%+v)", scale, style)
			}
		}
	}
}

func TestWindowsTextRasterizerRecordsTheAppScene(t *testing.T) {
	rasterizer := newPlatformTextRasterizer()
	display := &gpucontext.NullWindowProvider{W: 360, H: 200, SF: 1}
	a := app.New(app.WithTextRasterizer(rasterizer), app.WithWindowProvider(display))
	w := a.Window()
	const title = "ШИРОКАЯ КИРИЛЛИЦА"
	label := primitives.Text(title).FontSize(14)
	input := textfield.New(textfield.InitialValue("Ветка: исправление"))
	root := primitives.VBox(label, input)
	a.SetRoot(root)
	a.Frame()
	display.SF = 1.25
	w.HandleScaleChange(display.SF)
	a.Frame()
	app.PaintBoundaryLayersWithContext(root, nil, w.Context())

	recorded, ok := root.CachedScene().(*scene.Scene)
	if !ok || len(recorded.Images()) == 0 {
		t.Fatal("the app did not record native text as bitmaps")
	}
	expected, ok := rasterizer.Rasterize(title, widget.TextStyle{FontSize: 14, Color: widget.ColorBlack}, 1.25)
	if !ok {
		t.Fatal("could not rasterize the label at the window's scale")
	}
	if label.Bounds().Width() < float32(expected.Advance)/1.25 {
		t.Fatalf("the label's bounds clip its final glyph: %v", label.Bounds())
	}
	for _, img := range recorded.Images() {
		if img.Width == expected.Image.Rect.Dx() && img.Height == expected.Image.Rect.Dy() {
			return
		}
	}
	t.Fatal("the scene did not use the physical pixel size of the label")
}
