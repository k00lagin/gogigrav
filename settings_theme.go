package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

type importedTheme struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Colors map[string]string `json:"colors"`
}

func readTheme(path string) (*importedTheme, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const limit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("theme file exceeds 2 MB")
	}
	var theme importedTheme
	if err := json.Unmarshal(themeJSON(data), &theme); err != nil {
		return nil, fmt.Errorf("invalid VS Code theme (%w)", err)
	}
	if len(theme.Colors) == 0 {
		return nil, fmt.Errorf("no workbench colors found in the theme file")
	}
	if theme.Name == "" {
		theme.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return &theme, nil
}

func themeJSON(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		switch {
		case data[i] == '"':
			start := i
			i++
			for i < len(data) {
				ch := data[i]
				i++
				if ch == '\\' && i < len(data) {
					i++
				} else if ch == '"' {
					break
				}
			}
			out = append(out, data[start:i]...)
		case bytes.HasPrefix(data[i:], []byte("//")):
			end := bytes.IndexByte(data[i:], '\n')
			if end < 0 {
				return out
			}
			i += end
		case bytes.HasPrefix(data[i:], []byte("/*")):
			end := bytes.Index(data[i+2:], []byte("*/"))
			if end < 0 {
				return append(out, data[i:]...)
			}
			out = append(out, ' ')
			i += end + 4
		default:
			if data[i] == '}' || data[i] == ']' {
				trimmed := bytes.TrimRight(out, " \r\n\t")
				if n := len(trimmed); n > 0 && trimmed[n-1] == ',' {
					out = trimmed[:n-1]
				}
			}
			out = append(out, data[i])
			i++
		}
	}
	return out
}

func (t *importedTheme) theme(base *devtools.Theme) *devtools.Theme {
	dark := base.IsDark()
	switch strings.ToLower(t.Type) {
	case "light", "hclight", "vs", "hc-light":
		dark = false
	case "dark", "hc", "vs-dark", "hc-black", "hcdark":
		dark = true
	default:
		if bg, ok := t.color("editor.background"); ok {
			dark = bg.R*0.2126+bg.G*0.7152+bg.B*0.0722 < 0.5
		}
	}
	accent, ok := t.color("focusBorder", "button.background")
	if !ok {
		accent = devtools.DefaultAccentColor
	}
	newTheme := devtools.NewTheme
	if dark {
		newTheme = devtools.NewDarkTheme
	}
	result := newTheme(devtools.WithAccentColor(accent))
	cs := &result.Colors
	assign := func(target *widget.Color, keys ...string) {
		if color, ok := t.color(keys...); ok {
			*target = color
		}
	}
	assign(&cs.Background, "editor.background")
	assign(&cs.Surface, "sideBar.background", "panel.background")
	assign(&cs.SurfaceElevated, "editorWidget.background", "dropdown.background")
	assign(&cs.OnSurface, "foreground", "editor.foreground")
	assign(&cs.OnSurfaceSecondary, "descriptionForeground")
	assign(&cs.OnSurfaceDisabled, "disabledForeground")
	assign(&cs.OnPrimary, "button.foreground")
	assign(&cs.Selection, "list.activeSelectionBackground", "editor.selectionBackground")
	assign(&cs.Border, "panel.border", "editorWidget.border")
	assign(&cs.BorderStrong, "input.border", "dropdown.border")
	assign(&cs.InputBackground, "input.background")
	assign(&cs.ControlFill, "button.secondaryBackground", "dropdown.background")
	assign(&cs.ControlHover, "list.hoverBackground")
	assign(&cs.ScrollbarThumb, "scrollbarSlider.background")
	assign(&cs.ScrollbarThumbHover, "scrollbarSlider.hoverBackground")
	return result
}

func (t *importedTheme) color(keys ...string) (widget.Color, bool) {
	for _, key := range keys {
		hex, ok := strings.CutPrefix(t.Colors[key], "#")
		if !ok {
			continue
		}
		if len(hex) == 3 || len(hex) == 4 {
			var expanded strings.Builder
			for _, digit := range hex {
				expanded.WriteRune(digit)
				expanded.WriteRune(digit)
			}
			hex = expanded.String()
		}
		if value, err := strconv.ParseUint(hex, 16, 32); err == nil {
			switch len(hex) {
			case 6:
				return widget.Hex(uint32(value)), true
			case 8:
				return widget.HexA(uint32(value)), true
			}
		}
	}
	return widget.Color{}, false
}
