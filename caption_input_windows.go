package main

import (
	"log"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	setCaptionHook    = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowsHookExW")
	nextCaptionHook   = windows.NewLazySystemDLL("user32.dll").NewProc("CallNextHookEx")
	removeCaptionHook = windows.NewLazySystemDLL("user32.dll").NewProc("UnhookWindowsHookEx")
)

// MSG, inspected when Gogpu takes a native mouse message off the queue.
type captionWindowMessage struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   [2]int32
	private uint32
}

func (m *captionWindowMessage) clicksCaption() bool {
	const (
		wmNCLButtonDown = 0x00A1
		wmNCRButtonDown = 0x00A4
		htCaption       = 2
	)
	return m.wParam == htCaption && (m.message == wmNCLButtonDown || m.message == wmNCRButtonDown)
}

func (w *workbench) watchCaptionClicks() func() {
	// Run and hook Gogpu on the same OS thread as its native window.
	runtime.LockOSThread()
	const (
		whGetMessage = 3
		pmRemove     = 1
	)
	callback := windows.NewCallback(func(code int32, action uintptr, message *captionWindowMessage) uintptr {
		if code >= 0 && action == pmRemove && message != nil && message.clicksCaption() {
			var class [32]uint16
			n, err := windows.GetClassName(message.hwnd, &class[0], int32(len(class)))
			if err == nil && windows.UTF16ToString(class[:n]) == "GoGPUWindow" {
				w.dismissCaptionOverlays()
			}
		}
		// Keep the native move loop, caption double-click and system menu.
		result, _, _ := nextCaptionHook.Call(0, uintptr(code), action, uintptr(unsafe.Pointer(message)))
		return result
	})
	hook, _, err := setCaptionHook.Call(whGetMessage, callback, 0, uintptr(windows.GetCurrentThreadId()))
	if hook == 0 {
		runtime.UnlockOSThread()
		log.Printf("Caption input hook: %v", err)
		return func() {}
	}
	return func() {
		removeCaptionHook.Call(hook)
		runtime.UnlockOSThread()
	}
}
