package main

import (
	_ "embed"
	"encoding/json"
	"path"
	"strconv"
	"strings"

	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

//go:embed assets/filetree/icons.json
var fileIconJSON []byte

type fileIcon struct {
	image       icon.IconData
	light, dark widget.Color
}

type fileIconSet struct {
	icons               map[string]fileIcon
	byName, byExtension map[string]string
}

var fileIcons = loadFileIcons()

func loadFileIcons() fileIconSet {
	var asset struct {
		Icons map[string]struct {
			SVG         string
			Light, Dark uint32
		}
		ByName, ByExtension map[string]string
	}
	if err := json.Unmarshal(fileIconJSON, &asset); err != nil {
		panic(err)
	}
	icons := make(map[string]fileIcon, len(asset.Icons))
	for name, entry := range asset.Icons {
		icons[name] = fileIcon{icon.FromSVGXML("file-"+name, []byte(entry.SVG)), widget.Hex(entry.Light), widget.Hex(entry.Dark)}
	}
	return fileIconSet{icons, asset.ByName, asset.ByExtension}
}

func (s fileIconSet) forPath(file string) fileIcon {
	name := strings.ToLower(path.Base(file))
	if token := s.byName[name]; token != "" {
		return s.icons[token]
	}
	// Check compound extensions first, matching Pierre's filename resolver.
	for {
		_, extension, ok := strings.Cut(name, ".")
		if !ok {
			return s.icons["default"]
		}
		if token := s.byExtension[extension]; token != "" {
			return s.icons[token]
		}
		name = extension
	}
}

type fileTreePainter struct {
	devtools.TreeViewPainter
	dark                        bool
	colors                      map[byte]widget.Color
	actions, stage, showActions bool
}

func (p *fileTreePainter) PaintRowBackground(canvas widget.Canvas, state treeview.RowPaintState) {
	p.TreeViewPainter.PaintRowBackground(canvas, state)
	p.showActions = p.actions && !state.Disabled && (state.Hovered || state.Focused)
}

func (p fileTreePainter) PaintLabel(canvas widget.Canvas, state treeview.LabelState) {
	if p.showActions {
		discard, change := fileRowButtons(state.Bounds)
		fg := p.Theme.Colors.OnSurfaceSecondary
		glyph := icon.Minus
		if p.stage {
			glyph = icon.ExpAdd
		}
		paint := func(image icon.IconData, bounds geometry.Rect) {
			center := bounds.Center()
			icon.Draw(canvas, image, geometry.NewRect(center.X-7, center.Y-7, 14, 14), fg)
		}
		paint(discardFileIcon, discard)
		paint(glyph, change)
	}
	if state.Node != nil {
		if item, ok := state.Node.Data.(*fileItem); ok {
			p.paintFileLabel(canvas, state, item)
			return
		}
		if !state.Node.IsLeaf() {
			p.paintFolderLabel(canvas, state)
			return
		}
	}
	p.TreeViewPainter.PaintLabel(canvas, state)
}

func (p fileTreePainter) paintFolderLabel(canvas widget.Canvas, state treeview.LabelState) {
	const iconSize, gap = 16, 5
	bounds := state.Bounds
	if bounds.IsEmpty() || bounds.Width() <= iconSize+gap {
		p.TreeViewPainter.PaintLabel(canvas, state)
		return
	}
	fg := p.Theme.Colors.OnSurface
	iconColor := p.Theme.Colors.OnSurfaceSecondary
	if state.Disabled {
		fg = p.Theme.Colors.OnSurfaceDisabled
		iconColor = p.Theme.Colors.OnSurfaceDisabled
	}
	icon.Draw(canvas, icon.ExpFolder, geometry.NewRect(bounds.Min.X, bounds.Center().Y-iconSize/2, iconSize, iconSize), iconColor)
	bounds.Min.X += iconSize + gap
	if p.showActions {
		discard, _ := fileRowButtons(state.Bounds)
		bounds.Max.X = max(bounds.Min.X, discard.Min.X-5)
	}
	canvas.PushClip(bounds)
	canvas.DrawText(state.Text, bounds, 13, fg, false, widget.TextAlignLeft)
	canvas.PopClip()
}

func (p fileTreePainter) paintFileLabel(canvas widget.Canvas, state treeview.LabelState, item *fileItem) {
	const iconSize, gap = 16, 5
	bounds := state.Bounds
	if bounds.IsEmpty() {
		return
	}
	status, stat := item.change()
	color := p.colors[status]
	if color == (widget.Color{}) {
		color = p.colors['M']
	}
	right := bounds.Max.X - 9
	if status != 0 && status != ' ' {
		marker := string(status)
		if status == '?' {
			marker = "U"
		}
		statusBounds := geometry.NewRect(right-13, bounds.Min.Y, 13, bounds.Height())
		canvas.DrawText(marker, statusBounds, 12, color, true, widget.TextAlignCenter)
		right = statusBounds.Min.X - 4
	}
	if p.showActions {
		discard, _ := fileRowButtons(state.Bounds)
		right = discard.Min.X
	} else if stat.Text {
		const statGap = 6
		added := "+" + strconv.Itoa(stat.Added)
		deleted := "−" + strconv.Itoa(stat.Deleted)
		addedWidth, deletedWidth := canvas.MeasureText(added, 12, false), canvas.MeasureText(deleted, 12, false)
		if right-addedWidth-statGap-deletedWidth > bounds.Min.X+iconSize+gap+20 {
			right -= deletedWidth
			canvas.DrawText(deleted, geometry.NewRect(right, bounds.Min.Y, deletedWidth, bounds.Height()), 12, p.colors['D'], false, widget.TextAlignLeft)
			right -= statGap + addedWidth
			canvas.DrawText(added, geometry.NewRect(right, bounds.Min.Y, addedWidth, bounds.Height()), 12, p.colors['A'], false, widget.TextAlignLeft)
		}
	}
	bounds.Max.X = right - gap
	if bounds.Width() <= iconSize+gap {
		return
	}
	glyph := fileIcons.forPath(item.file.Path)
	iconColor := glyph.light
	if p.dark {
		iconColor = glyph.dark
	}
	icon.Draw(canvas, glyph.image, geometry.NewRect(bounds.Min.X, bounds.Center().Y-iconSize/2, iconSize, iconSize), iconColor)
	bounds.Min.X += iconSize + gap
	canvas.PushClip(bounds)
	canvas.DrawText(state.Text, bounds, 13, color, false, widget.TextAlignLeft)
	canvas.PopClip()
}

func (w *workbench) fileChangeColors() map[byte]widget.Color {
	colors := make(map[byte]widget.Color)
	settings := w.prefs.Settings
	for _, entry := range []struct {
		status      byte
		name        string
		light, dark uint32
	}{
		{'A', "added", 0x587c0c, 0x81b88b},
		{'M', "modified", 0x895503, 0xe2c08d},
		{'D', "deleted", 0xad0707, 0xc74e39},
		{'R', "renamed", 0x007100, 0x73c991},
		{'?', "untracked", 0x007100, 0x73c991},
		{'U', "conflicting", 0xad0707, 0xe4676b},
	} {
		value := entry.light
		if w.theme.IsDark() {
			value = entry.dark
		}
		colors[entry.status] = widget.Hex(value)
		if settings.UseImportedTheme && settings.ImportedTheme != nil {
			if color, ok := settings.ImportedTheme.color("gitDecoration." + entry.name + "ResourceForeground"); ok {
				colors[entry.status] = color
			}
		}
	}
	colors['C'] = colors['R']
	return colors
}
