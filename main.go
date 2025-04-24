package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, s.name, s.args...)
	fmt.Println(cmd.Args)
	s.cancel = cancel
	s.cmd = cmd
	s.errorChan = make(chan bool, 1)
	s.successChan = make(chan bool, 1)
	if err := cmd.Start(); err != nil {
		s.errorChan <- true
		return
	}

	if err := cmd.Wait(); err != nil {
		fmt.Println(err)
		s.errorChan <- true
	} else {
		s.successChan <- true
	}
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
			fmt.Printf("[%s] Script failed, retrying in 5s...\n", s.name)
			time.Sleep(5 * time.Second)
		}
	}
}

type WindowsService struct {
	name    string
	scripts []Script
}

func (m *WindowsService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	go m.startApp()

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
	for _, script := range m.scripts {
		go script.run()
	}
	select {}
}

func (m *WindowsService) stopApp() {
	for _, script := range m.scripts {
		script.cmd.Process.Kill()
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
	fullCmd := fmt.Sprintf(`%s %s --name="%s"`, exePath, args, name) // Removed the single quotes around %s for name
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
			installParams += fmt.Sprintf(`--command="%s" `, v)
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
	}

	runService(service, isService)
}
