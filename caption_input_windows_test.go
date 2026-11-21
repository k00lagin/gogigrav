package main

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/primitives"
	"golang.org/x/sys/windows"
)

func TestNativeCaptionClickDismissesOverlay(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	user32 := windows.NewLazySystemDLL("user32.dll")
	module, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	name := windows.StringToUTF16Ptr("GoGPUWindow")
	class := struct {
		style                uint32
		windowProc           uintptr
		classExtra, wndExtra int32
		instance, icon       uintptr
		cursor, background   uintptr
		menuName, className  *uint16
	}{windowProc: user32.NewProc("DefWindowProcW").Addr(), instance: module, className: name}
	if atom, _, err := user32.NewProc("RegisterClassW").Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		t.Fatalf("register test window class: %v", err)
	}
	defer user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), module)
	const hwndMessage = ^uintptr(2)
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0, 0, hwndMessage, 0, module, 0)
	if hwnd == 0 {
		t.Fatalf("create test window: %v", err)
	}
	defer user32.NewProc("DestroyWindow").Call(hwnd)

	w := &workbench{ui: app.New()}
	w.ui.SetRoot(primitives.Box())
	win := w.ui.Window()
	win.HandleResize(1080, 520)
	modal := dialog.New(dialog.Title("Caption click"))
	modal.Show(win.Context())
	if !modal.IsOpen() {
		t.Fatal("test dialog did not open")
	}
	unhook := w.watchCaptionClicks()
	defer unhook()

	const wmNCLButtonDown = 0x00A1
	const pmRemove = 1
	post := user32.NewProc("PostMessageW")
	peek := user32.NewProc("PeekMessageW")
	for _, hit := range []uintptr{12, 2} { // HTTOP, then HTCAPTION
		if ok, _, err := post.Call(hwnd, wmNCLButtonDown, hit, 0); ok == 0 {
			t.Fatalf("post non-client click: %v", err)
		}
		var message captionWindowMessage
		ok, _, _ := peek.Call(uintptr(unsafe.Pointer(&message)), hwnd, wmNCLButtonDown, wmNCLButtonDown, pmRemove)
		if ok == 0 || message.message != wmNCLButtonDown || message.wParam != hit {
			t.Fatal("native mouse message should still be available to the window procedure")
		}
		if modal.IsOpen() != (hit == 12) {
			t.Fatalf("unexpected dialog state after a non-client click with hit result %d", hit)
		}
	}
}
