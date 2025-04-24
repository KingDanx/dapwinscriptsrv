package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/KingDanx/daplogger"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

var logger daplogger.Logger

type ParsedCommands []string

func (pc *ParsedCommands) String() string {
	return strings.Join(*pc, " ")
}

func (pc *ParsedCommands) Set(value string) error {
	*pc = append(*pc, value)
	return nil
}

type Script struct {
	name        string
	args        []string
	cmd         *exec.Cmd
	ctx         context.Context // Store the context
	cancel      context.CancelFunc
	errorChan   chan bool
	successChan chan bool
}

func (s *Script) ParseCommand(command string) {
	split := strings.Split(command, " ")
	for i, v := range split {
		if i == 0 {
			s.name = v
		} else {
			s.args = append(s.args, v)
		}
	}
}

func (s *Script) spawnProcess() {
	s.ctx, s.cancel = context.WithCancel(context.Background()) // Create and store the context
	cmd := exec.CommandContext(s.ctx, s.name, s.args...)
	fmt.Println("Spawning with context:", cmd.Path, cmd.Args)
	s.cmd = cmd
	s.errorChan = make(chan bool, 1)
	s.successChan = make(chan bool, 1)

	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Println("Error starting:", err)
			s.errorChan <- true
			return
		}

		// Wait for either the process to finish or the context to be cancelled
		err := cmd.Wait()

		select {
		case <-s.ctx.Done():
			fmt.Printf("[%s] Context cancelled, attempting to kill process...\n", s.name)
			if s.cmd.Process != nil {
				killErr := s.cmd.Process.Kill()
				if killErr != nil {
					fmt.Println("Error killing process due to context cancellation:", killErr)
				} else {
					fmt.Println("Process killed due to context cancellation.")
				}
			}
			if err == nil || s.ctx.Err() == context.Canceled {
				s.errorChan <- true // Signal an error (cancellation)
			}
		default:
			// Context was not cancelled, process finished normally
			if err != nil {
				fmt.Println("Process exited with error:", err)
				s.errorChan <- true
			} else {
				s.successChan <- true
			}
		}
	}()
}

func (s *Script) run() {
	for {
		fmt.Printf("[%s] Starting script: %s %v\n", s.name, s.name, s.args)
		s.spawnProcess()

		select {
		case <-s.successChan:
			fmt.Printf("[%s] Script exited normally, not retrying\n", s.name)
			return
		case <-s.errorChan:
			fmt.Printf("[%s] Script failed or cancelled, retrying in 5s...\n", s.name)
			time.Sleep(5 * time.Second)
		}
		if s.ctx.Err() == context.Canceled {
			fmt.Printf("[%s] Run loop exiting due to context cancellation.\n", s.name)
			return
		}
	}
}

type WindowsService struct {
	name    string
	scripts []Script
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
				for _, script := range m.scripts {
					if script.cancel != nil {
						fmt.Printf("Cancelling context for script: %s\n", script.name)
						script.cancel() // Now this should not be nil (most of the time)
					} else {
						fmt.Printf("Cancel function is nil for script: %s\n", script.name)
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
		m.scripts[i].ctx = ctx
		m.scripts[i].cancel = cancel
		go m.scripts[i].run()
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
	fullCmd := fmt.Sprintf(`"%s" %s --name="%s"`, exePath, args, name)
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

//? ***************** functions related to flags

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

	var scripts []Script

	for _, v := range commands {
		script := Script{}
		script.ParseCommand(v)
		scripts = append(scripts, script)
	}
	fmt.Println(commands)
	fmt.Println(scripts)

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	if *install {
		installParams := ""
		for _, v := range commands {
			installParams += fmt.Sprintf(`--command=\"%s\" `, v)
		}
		fmt.Println(installParams)
		installService(exePath, *name, *desc, installParams)
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
