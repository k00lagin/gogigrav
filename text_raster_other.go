//go:build !windows

package main

import "github.com/gogpu/ui/widget"

func newPlatformTextRasterizer() widget.TextRasterizer { return nil }
