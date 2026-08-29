//go:build windows

package main

import (
	"context"

	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "PulseAgent"

func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

func runWindowsService() error {
	return svc.Run(windowsServiceName, &windowsService{})
}

func restartAgentService(_ context.Context, _ string) error { return nil }

type windowsService struct{}

func (s *windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- runWithContext(ctx, false) }()

	statuses <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}
	for {
		select {
		case err := <-runErr:
			if err != nil {
				return false, 1
			}
			statuses <- svc.Status{State: svc.StopPending}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-runErr; err != nil {
					return false, 1
				}
				return false, 0
			}
		}
	}
}
