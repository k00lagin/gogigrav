//go:build !windows

package main

import "github.com/gogpu/ui/widget"

func installationCLI() func() bool                    { return func() bool { return false } }
func (*workbench) installationControl() widget.Widget { return nil }
