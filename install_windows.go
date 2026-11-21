package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const currentVersionKey = `Software\Microsoft\Windows\CurrentVersion`

type installation struct {
	current, target, shortcut string
	uninstallKey              string
}

func appInstallation() (installation, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return installation{}, err
	}
	current, err := os.Executable()
	if err != nil {
		return installation{}, err
	}
	programs, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return installation{}, err
	}
	return installation{
		current: current, target: filepath.Join(base, "Gogigrav", "Gogigrav.exe"),
		shortcut:     filepath.Join(programs, "Gogigrav.lnk"),
		uninstallKey: currentVersionKey + `\Uninstall\Gogigrav`,
	}, nil
}

func (i installation) registered() bool {
	for _, path := range []string{i.target, i.shortcut} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, i.uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue("UninstallString")
	return err == nil
}

func (i installation) install() (result error) {
	lock, err := i.lock()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(lock)
	var finish []func(bool) error
	defer func() {
		commit := result == nil
		for j := len(finish) - 1; j >= 0; j-- {
			result = errors.Join(result, finish[j](commit))
		}
	}()

	if !samePath(i.current, i.target) {
		file, err := stageCopy(i.current, filepath.Dir(i.target))
		if err != nil {
			return fmt.Errorf("подготовка Gogigrav.exe: %w", err)
		}
		defer os.Remove(file)
		change, err := replaceStaged(file, i.target)
		if err != nil {
			return fmt.Errorf("замена Gogigrav.exe: %w", err)
		}
		finish = append(finish, change)
	}

	dir, err := os.MkdirTemp(filepath.Dir(i.shortcut), ".gogigrav-install-")
	if err != nil {
		return err
	}
	defer os.Remove(dir)
	link := filepath.Join(dir, "Gogigrav.lnk")
	defer os.Remove(link)
	if err := createShortcut(link, i.target); err != nil {
		return fmt.Errorf("создание ярлыка в меню «Пуск»: %w", err)
	}
	change, err := replaceStaged(link, i.shortcut)
	if err != nil {
		return err
	}
	finish = append(finish, change)
	return i.register()
}

func (i installation) register() error {
	key, existed, err := registry.CreateKey(registry.CURRENT_USER, i.uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	command := windows.ComposeCommandLine([]string{i.target, "-uninstall"})
	for name, value := range map[string]string{
		"DisplayName": "Gogigrav", "DisplayIcon": i.target, "InstallLocation": filepath.Dir(i.target),
		"UninstallString": command, "QuietUninstallString": command,
	} {
		if err = key.SetStringValue(name, value); err != nil {
			break
		}
	}
	if err == nil {
		err = key.SetDWordValue("NoModify", 1)
	}
	if err == nil {
		err = key.SetDWordValue("NoRepair", 1)
	}
	key.Close()
	if err != nil && !existed {
		err = errors.Join(err, registry.DeleteKey(registry.CURRENT_USER, i.uninstallKey))
	}
	return err
}

func (i installation) uninstall() error {
	lock, err := i.lock()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(lock)
	if samePath(i.current, i.target) {
		if err := i.checkOtherInstances(); err != nil {
			return err
		}
		return i.removeAfterExit(lock)
	}
	if err := ignoreMissing(os.Remove(i.target)); err != nil {
		return err
	}
	if err := ignoreMissing(os.Remove(i.shortcut)); err != nil {
		return err
	}
	return ignoreMissing(registry.DeleteKey(registry.CURRENT_USER, i.uninstallKey))
}

// When uninstalling from Installed Apps, the uninstaller itself maps target.
// Check for other instances before handing deletion to the post-exit helper.
func (i installation) checkOtherInstances() error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	name := filepath.Base(i.target)
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == uint32(os.Getpid()) || !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			continue
		}
		process, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if e != nil {
			continue
		}
		var path [32768]uint16
		size := uint32(len(path))
		e = windows.QueryFullProcessImageName(process, 0, &path[0], &size)
		windows.CloseHandle(process)
		if e == nil && samePath(windows.UTF16ToString(path[:size]), i.target) {
			return errors.New("close the installed copy of Gogigrav and retry")
		}
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil
	}
	return err
}

func (i installation) removeAfterExit(lock windows.Handle) error {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	// The installed executable is mapped until this process exits. Use a
	// hidden helper to remove that exact file and its Installed Apps entry.
	const script = `$ErrorActionPreference = 'Stop'
$parent = Get-Process -Id ([int]$env:GOGIGRAV_UNINSTALL_PID) -ErrorAction SilentlyContinue
if ($parent -and -not $parent.WaitForExit(20000)) { exit 1 }
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    if (-not (Test-Path -LiteralPath $env:GOGIGRAV_UNINSTALL_EXE)) { break }
    try { Remove-Item -LiteralPath $env:GOGIGRAV_UNINSTALL_EXE -Force; break }
    catch { Start-Sleep -Milliseconds 100 }
	}
if (Test-Path -LiteralPath $env:GOGIGRAV_UNINSTALL_EXE) { exit 1 }
if (Test-Path -LiteralPath $env:GOGIGRAV_UNINSTALL_LINK) { Remove-Item -LiteralPath $env:GOGIGRAV_UNINSTALL_LINK }
if (Test-Path -LiteralPath $env:GOGIGRAV_UNINSTALL_KEY) { Remove-Item -LiteralPath $env:GOGIGRAV_UNINSTALL_KEY }
`
	cmd := exec.Command(filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"),
		"-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(utf16Bytes(script)))
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOGIGRAV_UNINSTALL_PID=%d", os.Getpid()),
		"GOGIGRAV_UNINSTALL_EXE="+i.target, "GOGIGRAV_UNINSTALL_LINK="+i.shortcut, `GOGIGRAV_UNINSTALL_KEY=HKCU:\`+i.uninstallKey)
	current := windows.CurrentProcess()
	var inherited windows.Handle
	if err := windows.DuplicateHandle(current, lock, current, &inherited, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return err
	}
	defer windows.CloseHandle(inherited)
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW,
		AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(inherited)}}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (i installation) lock() (windows.Handle, error) {
	dir := filepath.Dir(i.target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	return windows.CreateFile(wide(filepath.Join(dir, ".install.lock")), windows.GENERIC_READ|windows.DELETE, 0,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_FLAG_DELETE_ON_CLOSE|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}

func stageCopy(source, dir string) (path string, result error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.CreateTemp(dir, ".gogigrav-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if result != nil {
			_ = os.Remove(output.Name())
		}
	}()
	_, err = io.Copy(output, input)
	if err == nil {
		err = output.Sync()
	}
	return output.Name(), errors.Join(err, output.Close())
}

func replaceStaged(staged, target string) (func(bool) error, error) {
	var backup string
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s не является обычным файлом", target)
		}
		backup, err = stageCopy(target, filepath.Dir(target))
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := replaceFile(staged, target); err != nil {
		if backup != "" {
			_ = os.Remove(backup)
		}
		return nil, err
	}
	return func(commit bool) error {
		if !commit {
			if backup != "" {
				return replaceFile(backup, target)
			}
			return os.Remove(target)
		}
		if backup != "" {
			return os.Remove(backup)
		}
		return nil
	}, nil
}

func replaceFile(from, to string) error {
	return windows.MoveFileEx(wide(from), wide(to), windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func samePath(first, second string) bool {
	return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
}

func ignoreMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	return err
}

func utf16Bytes(text string) []byte {
	var data []byte
	for _, value := range utf16.Encode([]rune(text)) {
		data = binary.LittleEndian.AppendUint16(data, value)
	}
	return data
}

func installationCLI() func() bool {
	install := flag.Bool("install", false, "install or update Gogigrav for the current Windows user")
	uninstall := flag.Bool("uninstall", false, "remove the installed copy of Gogigrav")
	return func() bool {
		if !*install && !*uninstall {
			return false
		}
		i, err := appInstallation()
		switch {
		case *install && *uninstall:
			err = errors.New("use -install and -uninstall separately")
		case err != nil:
		case *install:
			err = i.install()
		default:
			err = i.uninstall()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			windows.MessageBox(0, wide(err.Error()), wide("Gogigrav setup"), windows.MB_ICONERROR)
			os.Exit(1)
		}
		return true
	}
}

func (w *workbench) installationControl() widget.Widget {
	i, err := appInstallation()
	if err != nil {
		return w.muted(err.Error())
	}
	label := "Install for this user"
	if i.registered() {
		if samePath(i.current, i.target) {
			return w.muted("Installed for this user")
		}
		label = "Update installed copy"
	}
	text, disabled := state.NewSignal(label), state.NewSignal(false)
	return button.New(button.TextSignal(text), button.DisabledSignal(disabled), button.SizeOpt(button.Small), button.VariantOpt(button.Outlined), button.PainterOpt(w.paint.Button), button.OnClick(func() {
		disabled.Set(true)
		w.runJob(func(context.Context) (func(), error) { return nil, i.install() }, func(_ func(), err error) {
			if err != nil {
				disabled.Set(false)
				w.showError(err)
				return
			}
			text.Set("Installed for this user")
		})
	}))
}

func wide(value string) *uint16 {
	p, _ := windows.UTF16PtrFromString(value)
	return p
}
