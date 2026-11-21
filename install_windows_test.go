package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func testInstallation(t *testing.T) installation {
	t.Helper()
	root := t.TempDir()
	key := fmt.Sprintf(`Software\GogigravTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	i := installation{
		current:  filepath.Join(root, "portable copy.exe"),
		target:   filepath.Join(root, "Gogigrav's installed copy", "Gogigrav.exe"),
		shortcut: filepath.Join(root, "Gogigrav.lnk"), uninstallKey: key + `\Installed`,
	}
	t.Cleanup(func() {
		for _, path := range []string{i.uninstallKey, key} {
			if err := ignoreMissing(registry.DeleteKey(registry.CURRENT_USER, path)); err != nil {
				t.Error(err)
			}
		}
	})
	return i
}

func TestInstallRegistersShortcutAndRetainsData(t *testing.T) {
	i := testInstallation(t)
	content := []byte("portable program")
	if err := os.WriteFile(i.current, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := i.install(); err != nil {
		t.Fatal(err)
	}
	if !i.registered() {
		t.Fatal("missing Installed Apps registration")
	}
	if got, err := os.ReadFile(i.target); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("installed program = %q, error %v", got, err)
	}
	link, err := os.ReadFile(i.shortcut)
	if err != nil || len(link) < 76 || binary.LittleEndian.Uint32(link) != 76 {
		t.Fatalf("expected a Windows Shell Link, error %v", err)
	}
	data := filepath.Join(filepath.Dir(i.target), "settings.json")
	if err := os.WriteFile(data, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(i.current, []byte("new build"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := i.install(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(i.target); err != nil || string(got) != "new build" {
		t.Fatalf("updated executable = %q, %v", got, err)
	}
	if err := i.uninstall(); err != nil {
		t.Fatal(err)
	}
	assertUninstalled(t, i, data)
}

func TestFailedInstallationRestoresExecutable(t *testing.T) {
	i := testInstallation(t)
	for path, data := range map[string]string{i.current: "new build", i.target: "installed build"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	i.shortcut = filepath.Join(filepath.Dir(i.shortcut), "missing", "Gogigrav.lnk")
	if err := i.install(); err == nil {
		t.Fatal("installation succeeded with an invalid shortcut directory")
	}
	if got, err := os.ReadFile(i.target); err != nil || string(got) != "installed build" {
		t.Fatalf("prior installed executable was not restored: %q, %v", got, err)
	}
}

func TestUninstallFromInstalledExecutable(t *testing.T) {
	i := testInstallation(t)
	var err error
	i.current, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := i.install(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(filepath.Dir(i.target), "settings.json")
	if err := os.WriteFile(data, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), i.target, "-test.run=^TestInstalledUninstallChild$")
	cmd.Env = append(os.Environ(), "GOGIGRAV_TEST_SHORTCUT="+i.shortcut, "GOGIGRAV_TEST_UNINSTALL_KEY="+i.uninstallKey)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installed executable could not start uninstallation: %v\n%s", err, output)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, fileErr := os.Stat(i.target)
		key, keyErr := registry.OpenKey(registry.CURRENT_USER, i.uninstallKey, registry.QUERY_VALUE)
		if keyErr == nil {
			key.Close()
		}
		if errors.Is(fileErr, os.ErrNotExist) && errors.Is(keyErr, registry.ErrNotExist) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assertUninstalled(t, i, data)
}

func TestInstalledUninstallChild(t *testing.T) {
	if os.Getenv("GOGIGRAV_TEST_SHORTCUT") == "" {
		t.Skip("runs only as the installed executable")
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	i := installation{current: current, target: current, shortcut: os.Getenv("GOGIGRAV_TEST_SHORTCUT"),
		uninstallKey: os.Getenv("GOGIGRAV_TEST_UNINSTALL_KEY")}
	if err := i.uninstall(); err != nil {
		t.Fatal(err)
	}
}

func assertUninstalled(t *testing.T, i installation, data string) {
	t.Helper()
	for _, path := range []string{i.target, i.shortcut} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %s to be removed, stat error %v", path, err)
		}
	}
	if key, err := registry.OpenKey(registry.CURRENT_USER, i.uninstallKey, registry.QUERY_VALUE); !errors.Is(err, registry.ErrNotExist) {
		if err == nil {
			key.Close()
		}
		t.Errorf("expected Installed Apps entry to be removed, open error %v", err)
	}
	if got, err := os.ReadFile(data); err != nil || string(got) != "database" {
		t.Errorf("uninstallation did not retain settings: %q, %v", got, err)
	}
}
