//go:build !windows

package main

import "errors"

func watchShutdownEvent(name string) (<-chan struct{}, func(), error) {
	if name != "" {
		return nil, nil, errors.New("shutdown-event is only supported on Windows; use SIGTERM")
	}
	return nil, func() {}, nil
}
