package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var coCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")

var (
	clsidShellLink = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = windows.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = windows.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

type unknownMethods struct{ queryInterface, addRef, release uintptr }

type shellLink struct {
	vtable *struct {
		unknownMethods
		getPath, getIDList, setIDList, getDescription, setDescription                  uintptr
		getWorkingDirectory, setWorkingDirectory, getArguments, setArguments           uintptr
		getHotkey, setHotkey, getShowCmd, setShowCmd, getIconLocation, setIconLocation uintptr
		setRelativePath, resolve, setPath                                              uintptr
	}
}

type persistFile struct {
	vtable *struct {
		unknownMethods
		getClassID, isDirty, load, save, saveCompleted, getCurrentFile uintptr
	}
}

func createShortcut(path, target string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && err != syscall.Errno(1) { // S_FALSE is also success.
		return fmt.Errorf("initialize Shell Link COM: %w", err)
	}
	defer windows.CoUninitialize()

	var link *shellLink
	result, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link)))
	if err := comResult("create IShellLinkW", result); err != nil {
		return err
	}
	defer syscall.SyscallN(link.vtable.release, uintptr(unsafe.Pointer(link)))
	for _, entry := range []struct {
		method uintptr
		value  string
	}{
		{link.vtable.setPath, target},
		{link.vtable.setWorkingDirectory, filepath.Dir(target)},
		{link.vtable.setDescription, "Gogigrav"},
	} {
		result, _, _ := syscall.SyscallN(entry.method, uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(wide(entry.value))))
		if err := comResult("configure Shell Link", result); err != nil {
			return err
		}
	}

	var file *persistFile
	result, _, _ = syscall.SyscallN(link.vtable.queryInterface, uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&file)))
	if err := comResult("get IPersistFile", result); err != nil {
		return err
	}
	defer syscall.SyscallN(file.vtable.release, uintptr(unsafe.Pointer(file)))
	result, _, _ = syscall.SyscallN(file.vtable.save, uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(wide(path))), 1)
	return comResult("save Shell Link", result)
}

func comResult(action string, result uintptr) error {
	if int32(result) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08x", action, uint32(result))
	}
	return nil
}
