package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsGracefulShutdownSignal(t *testing.T) {
	name := fmt.Sprintf(`Local\Kairisei-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	requested, cleanup, err := watchShutdownEvent(name)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ptr, _ := windows.UTF16PtrFromString(name)
	handle, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, ptr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	select {
	case <-requested:
		t.Fatal("shutdown before signal")
	default:
	}
	if err := windows.SetEvent(handle); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("shutdown signal not delivered")
	}
}
