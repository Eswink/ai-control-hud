//go:build windows

package winservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var ErrUnsupported = errors.New("Windows service lifecycle is unsupported")

type Runner func(context.Context) error

type Info struct {
	Installed bool
	State     string
	ProcessID uint32
}

func Install(name, displayName, description, executable, configPath string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()

	if existing, openErr := manager.OpenService(name); openErr == nil {
		existing.Close()
		return errors.New("AI Control Agent service is already installed")
	} else if !errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("query existing service: %w", openErr)
	}

	service, err := manager.CreateService(
		name,
		executable,
		mgr.Config{
			DisplayName: displayName,
			Description: description,
			StartType:   mgr.StartAutomatic,
		},
		"service", "run", "--config", configPath,
	)
	if err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	defer service.Close()
	return nil
}

func Start(name string) error {
	service, cleanup, err := open(name)
	if err != nil {
		return err
	}
	defer cleanup()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Running {
		return nil
	}
	if err := service.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return waitState(service, svc.Running, 20*time.Second)
}

func Stop(name string) error {
	service, cleanup, err := open(name)
	if err != nil {
		return err
	}
	defer cleanup()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop service: %w", err)
	}
	return waitState(service, svc.Stopped, 20*time.Second)
}

func Restart(name string) error {
	if err := Stop(name); err != nil {
		return err
	}
	return Start(name)
}

func Remove(name string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer service.Close()
	status, queryErr := service.Query()
	if queryErr == nil && status.State != svc.Stopped {
		_, _ = service.Control(svc.Stop)
		_ = waitState(service, svc.Stopped, 20*time.Second)
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("remove service: %w", err)
	}
	return nil
}

func Status(name string) (Info, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return Info{}, fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return Info{Installed: false, State: "not-installed"}, nil
	}
	if err != nil {
		return Info{}, fmt.Errorf("open service: %w", err)
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return Info{}, fmt.Errorf("query service: %w", err)
	}
	return Info{Installed: true, State: stateLabel(status.State), ProcessID: status.ProcessId}, nil
}

func Run(name string, runner Runner) error {
	if runner == nil {
		return errors.New("service runner is missing")
	}
	return svc.Run(name, &handler{runner: runner})
}

type handler struct {
	runner Runner
}

func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- h.runner(ctx) }()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-result:
			changes <- svc.Status{State: svc.StopPending}
			if err != nil {
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case err := <-result:
					if err != nil {
						return true, 1
					}
					return false, 0
				case <-time.After(10 * time.Second):
					return true, 2
				}
			}
		}
	}
}

func open(name string) (*mgr.Service, func(), error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connect service manager: %w", err)
	}
	service, err := manager.OpenService(name)
	if err != nil {
		manager.Disconnect()
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, nil, errors.New("AI Control Agent service is not installed")
		}
		return nil, nil, fmt.Errorf("open service: %w", err)
	}
	return service, func() {
		service.Close()
		manager.Disconnect()
	}, nil
}

func waitState(service *mgr.Service, wanted svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == wanted {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach %s state", stateLabel(wanted))
}

func stateLabel(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start-pending"
	case svc.StopPending:
		return "stop-pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue-pending"
	case svc.PausePending:
		return "pause-pending"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("unknown-%d", state)
	}
}
