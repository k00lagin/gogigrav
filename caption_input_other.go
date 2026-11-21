//go:build !windows

package main

func (w *workbench) watchCaptionClicks() func() { return func() {} }
