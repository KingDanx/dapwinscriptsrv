# dapwinscriptsrv

`dapwinscriptsrv` is a lightweight Windows service wrapper written in Go that allows you to run **one or more long-running scripts or executables as a Windows Service**, with automatic restart on failure and clean shutdown handling.

It is designed for running Node.js, Bun, Python, or any other CLI-based background processes reliably on Windows without relying on third-party service wrappers.

---

## Features

- Runs **multiple commands concurrently** under a single Windows Service
- Automatically restarts failed scripts
- Graceful shutdown using Windows Service stop signals
- Supports installation, uninstallation, start, and stop via CLI flags
- Works with **any executable** available on the system PATH
- Logs errors to disk when running as a service

---

## Requirements

- Windows 10 / Windows Server
- Administrator privileges (required to install/remove services)
- Executables you wish to run must be accessible via PATH or specified with full paths

---

## Building

```bash
go build -o dapwinscriptsrv.exe
```

---

## Installing the Service

Run the executable **as Administrator** to install the service:

```powershell
dapwinscriptsrv.exe `
  --install `
  --name "MyServiceName" `
  --description "Runs background scripts as a Windows service" `
  --command "node index.js" `
  --command "bun worker.js"
```

---

## Command Line Flags

| Flag            | Description                                                          |
| --------------- | -------------------------------------------------------------------- |
| `--install`     | Install the Windows service                                          |
| `--uninstall`   | Uninstall the Windows service                                        |
| `--start`       | Start the Windows service                                            |
| `--stop`        | Stop the Windows service                                             |
| `--name`        | Name of the Windows service (default: `dapwinscriptsrv`)             |
| `--description` | Service description shown in Windows Services                        |
| `--command`     | Command to run as a managed script (may be specified multiple times) |

---

## Command Syntax

Each `--command` flag defines **one process** that will be managed by the service.

### Format

```text
--command "executable arg1 arg2 arg3"
```

### Examples

```bash
--command "node index.js"
--command "bun run server.ts"
--command "python worker.py"
--command "C:\Tools\mybinary.exe --flag value"
```

- The **first token** is treated as the executable
- All remaining tokens are passed as arguments
- Commands are executed with the service executable’s directory as the working directory

---

## Running Multiple Scripts

You may specify the `--command` flag multiple times to run multiple scripts concurrently:

```powershell
dapwinscriptsrv.exe --install `
  --name "MultiScriptService" `
  --command "node api.js" `
  --command "bun queue-worker.ts" `
  --command "python scheduler.py"
```

Each script runs independently and is monitored separately.

---

## Starting and Stopping the Service

### Start

```powershell
dapwinscriptsrv.exe --start --name "MyServiceName"
```

### Stop

```powershell
dapwinscriptsrv.exe --stop --name "MyServiceName"
```

---

## Uninstalling the Service

```powershell
dapwinscriptsrv.exe --uninstall --name "MyServiceName"
```

---

## Runtime Behavior

- Each script runs in its own goroutine
- Scripts are started when the service enters the **Running** state
- If a script exits with an error, it will be restarted after **5 seconds**
- If a script exits successfully, it will **not be restarted**
- When the Windows service receives a stop or shutdown signal:
  - All script contexts are cancelled
  - Child processes are terminated if still running
  - The service shuts down cleanly

---

## Working Directory

All scripts are executed with the **directory of the service executable** as the working directory.  
This ensures predictable relative paths regardless of how the service is launched.

---

## Logging

### Service Mode

When running as a Windows Service:

- Logs are written to an `ErrorLogs` directory next to the executable
- Log files are rotated automatically
- Errors during startup or execution are persisted to disk

### Interactive Mode

When run manually from a terminal:

- Logs are written relative to the current working directory

---

## Example Use Cases

- Running a Node.js or Bun backend as a Windows Service
- Hosting background workers on Windows Server
- Replacing `node-windows`, NSSM, or Task Scheduler
- Managing multiple long-running processes under a single service

---

## Notes and Limitations

- Must be run as **Administrator** when installing or uninstalling services
- Intended for **non-interactive** background processes
- GUI applications are not supported
- All managed commands must be long-running processes

---

## License

MIT
