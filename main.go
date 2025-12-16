package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/KingDanx/daplogger"
	"github.com/KingDanx/dapwinscriptsrv/script"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

var logger daplogger.Logger = initLogging()

type ParsedCommands []string

func (pc *ParsedCommands) String() string {
	return strings.Join(*pc, " ")
}

func (pc *ParsedCommands) Set(value string) error {
	*pc = append(*pc, value)
	return nil
}

type WindowsService struct {
	name    string
	scripts []script.Script
	mu      sync.Mutex
}

func (m *WindowsService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	m.startApp()

	//? Immediately report the service as running
	s <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				fmt.Println("Stopping service, cancelling contexts...")
				for _, s := range m.scripts {
					if s.Cancel != nil {
						fmt.Printf("Cancelling context for script: %s\n", s.Name)
						s.Cancel() // Now this should not be nil (most of the time)
					} else {
						fmt.Printf("Cancel function is nil for script: %s\n", s.Name)
					}
				}
				s <- svc.Status{State: svc.StopPending}
				break loop
			default:
				log.Printf("unexpected control request #%d", c)
			}
		}
	}

	s <- svc.Status{State: svc.Stopped}
	return
}

func (m *WindowsService) startApp() {
	for i := range m.scripts {
		// Create context and cancel function *here*
		ctx, cancel := context.WithCancel(context.Background())
		m.scripts[i].Ctx = ctx
		m.scripts[i].Cancel = cancel
		go m.scripts[i].Run()
	}
}

func runService(service WindowsService, isService bool) {
	var err error
	run := debug.Run
	if isService {
		run = svc.Run
	}

	err = run(service.name, &service)
	if err != nil {
		logger.LogError(fmt.Sprintf("Failed to start the service %e", err))
		log.Fatalf("%s service failed: %v", service.name, err)
	}

	log.Printf("%s service stopped", service.name)
}

// ? ***************** functions related to flags
func installService(exePath, name, description, args string) {
	fullCmd := fmt.Sprintf(`"%s" %s`, exePath, args)
	fmt.Println("Full Command: ", fullCmd)
	installCmd := fmt.Sprintf(
		`New-Service -Name "%s" -BinaryPathName '%s' -StartupType Automatic -Description "%s"`,
		name, fullCmd, description,
	)

	fmt.Println(installCmd)
	cmd := exec.Command("powershell", "-Command", installCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to create service: %v. Output: %s", err, output)
	}

	fmt.Println(string(output))
}

func uninstallService(serviceName string) {
	cmd := exec.Command("sc", "delete", serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to remove service: %v. Output: %s", err, output)
	}

	fmt.Println(string(output))
	fmt.Println("Service uninstalled successfully.")
}

func startService(serviceName string) {
	startCmd := fmt.Sprintf(`Start-Service -Name "%s"`, serviceName)
	cmd := exec.Command("powershell", "-Command", startCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to start service: %v. Output: %s", err, output)
	}
	fmt.Println(string(output))
}

func stopService(serviceName string) {
	startCmd := fmt.Sprintf(`Stop-Service -Name "%s"`, serviceName)
	cmd := exec.Command("powershell", "-Command", startCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to stop service: %v. Output: %s", err, output)
	}
	fmt.Println(string(output))
}

func initLogging() daplogger.Logger {
	var cwd string

	isServce, err := svc.IsWindowsService()
	if err != nil {
		log.Fatal("Error determining service status")
	}

	if isServce {
		exePath, err := os.Executable()
		if err != nil {
			log.Fatalf("Failed to get executable path: %v", err)
		}

		cwd = filepath.Dir(exePath)
	} else {
		dir, err := os.Getwd()
		if err != nil {
			log.Fatal("Failed to get the working directory")
		}
		cwd = dir
	}

	logDir := path.Join(cwd, "ErrorLogs")

	logger := daplogger.CreateLogger(logDir, "ErrorLogs", 21)

	return logger
}

func main() {
	install := flag.Bool("install", false, "Install the service")
	uninstall := flag.Bool("uninstall", false, "Uninstall the service")
	start := flag.Bool("start", false, "Start the service")
	stop := flag.Bool("stop", false, "Stop the service")
	name := flag.String("name", "dapwinscriptsrv", "Name of the service")
	desc := flag.String("description", "Runs scripts as a Windows service", "What the application does")

	var commands ParsedCommands
	flag.Var(&commands, "command", "A command to run as a service: node,index.js")

	flag.Parse()

	var scripts []script.Script

	for _, v := range commands {
		script := script.Script{}
		script.ParseCommand(v)
		scripts = append(scripts, script)
		fmt.Println("Script:", script)
	}
	// fmt.Println(commands)
	// fmt.Println(scripts)

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	if *install {
		installParams := ""
		for _, v := range commands {
			installParams += fmt.Sprintf(`--command="%s" `, v)
		}
		fmt.Println(installParams)
		installService(exePath, *name, *desc, strings.TrimSpace(installParams))
		return
	}

	if *uninstall {
		uninstallService(*name)
		return
	}

	if *start {
		startService(*name)
		return
	}

	if *stop {
		stopService(*name)
		return
	}

	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}

	service := WindowsService{
		name:    *name,
		scripts: scripts,
		mu:      sync.Mutex{},
	}

	runService(service, isService)
}
