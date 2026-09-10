package main

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// Windows GUI launchers cannot deliver SIGTERM through Stop-Process. A named
// kernel event provides a local shutdown signal without exposing an HTTP API.
func watchShutdownEvent(name string) (<-chan struct{}, func(), error) {
	if name == "" {
		return nil, func() {}, nil
	}
	if !strings.HasPrefix(name, `Local\Kairisei-`) {
		return nil, nil, fmt.Errorf("invalid shutdown event name")
	}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, nil, err
	}
	handle, err := windows.CreateEvent(nil, 1, 0, ptr)
	if err != nil {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return nil, nil, err
	}
	requested, cancelled, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-cancelled:
				return
			default:
			}
			result, err := windows.WaitForSingleObject(handle, 250)
			if err != nil {
				return
			}
			if result == windows.WAIT_OBJECT_0 {
				close(requested)
				return
			}
		}
	}()
	return requested, func() { close(cancelled); <-done; windows.CloseHandle(handle) }, nil
}
