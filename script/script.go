package script

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Script struct {
	Name        string
	args        []string
	cmd         *exec.Cmd
	Ctx         context.Context
	Cancel      context.CancelFunc
	errorChan   chan bool
	successChan chan bool
}

func (s *Script) ParseCommand(command string) {
	var parts []string
	var current strings.Builder
	inQuotes := false

	// Iterate over each character in the command
	for i := 0; i < len(command); i++ {
		c := command[i]

		switch c {
		case '|':
			// Toggle the inQuotes flag when encountering a single quote
			inQuotes = !inQuotes
			if len(parts) != 0 {
			}
		case ' ':
			// If inside quotes, continue accumulating the argument
			if inQuotes {
				current.WriteByte(c)
			} else if current.Len() > 0 {
				// If we encounter a space and we're outside of quotes, finalize the current argument
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			// Otherwise, accumulate the character in the current argument
			current.WriteByte(c)
		}
	}

	// Add last part if needed
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	// First argument is the command
	if len(parts) > 0 {
		s.Name = parts[0]
		s.args = parts[1:]
	}
}

func (s *Script) Run() {
	for {
		fmt.Printf("[%s] Starting script: %s %v\n", s.Name, s.Name, s.args)
		s.spawnProcess()

		select {
		case <-s.successChan:
			fmt.Printf("[%s] Script exited normally, not retrying\n", s.Name)
			return
		case <-s.errorChan:
			fmt.Printf("[%s] Script failed or cancelled, retrying in 5s...\n", s.Name)
			time.Sleep(5 * time.Second)
		}
		if s.Ctx.Err() == context.Canceled {
			fmt.Printf("[%s] Run loop exiting due to context cancellation.\n", s.Name)
			return
		}
	}
}

func (s *Script) spawnProcess() {
	s.Ctx, s.Cancel = context.WithCancel(context.Background()) // Create and store the context
	cmd := exec.CommandContext(s.Ctx, s.Name, s.args...)
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	cmd.Dir = filepath.Dir(exePath)

	fmt.Println("Spawning with context:", cmd.Path, cmd.Args)
	s.cmd = cmd
	s.errorChan = make(chan bool, 1)
	s.successChan = make(chan bool, 1)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	go func() {
		if err := cmd.Start(); err != nil {
			s.errorChan <- true
			return
		}

		// Wait for either the process to finish or the context to be cancelled
		err := cmd.Wait()

		select {
		case <-s.Ctx.Done():
			if s.cmd.Process != nil {
				s.cmd.Process.Kill()
			}
			if err == nil || s.Ctx.Err() == context.Canceled {
				s.errorChan <- true // Signal an error (cancellation)
			}
		default:
			// Context was not cancelled, process finished normally
			if err != nil {
				s.errorChan <- true
			} else {
				s.successChan <- true
			}
		}
	}()
}
