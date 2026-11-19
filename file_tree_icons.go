package main

import (
	_ "embed"
	"encoding/json"
	"path"
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
	dark bool
}

func (p fileTreePainter) PaintLabel(canvas widget.Canvas, state treeview.LabelState) {
	const size, gap = 16, 5
	if state.Node != nil {
		if item, ok := state.Node.Data.(*fileItem); ok && state.Bounds.Width() > size+gap {
			glyph := fileIcons.forPath(item.file.Path)
			color := glyph.light
			if p.dark {
				color = glyph.dark
			}
			if state.Disabled {
				color = color.WithAlpha(0.5)
			}
			bounds := geometry.NewRect(state.Bounds.Min.X, state.Bounds.Center().Y-size/2, size, size)
			icon.Draw(canvas, glyph.image, bounds, color)
			state.Bounds.Min.X += size + gap
		}
	}
	p.TreeViewPainter.PaintLabel(canvas, state)
}
