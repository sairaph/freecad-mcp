# FreeCAD MCP

FreeCAD MCP lets an AI client build, check and export FreeCAD models through structured tools, with a guide skill for its workflows. Models stay parametric, and every change reports what it did to the shape.

## What it does

- Parts and booleans.
- PartDesign bodies with sketches, pads, pockets, holes, fillets and patterns.
- Spreadsheet-driven parameters.
- Assemblies of App::Parts.
- Measuring, and choosing faces and edges.
- 3D printing: material guides (PETG, PLA) for fits, threads, clips and inserts, a printability check against the printer's build volume, and STEP, STL and 3MF export.
- Mesh import, repair and conversion to solids.
- FEM with CalculiX.
- Sharing one FreeCAD between agents and computers.
- Python scripts for anything the tools do not cover.

## Requirements

FreeCAD 1.0 or newer on the computer that runs it. It is tested mainly on Windows with FreeCAD 1.1; macOS and Linux builds are provided and less tested. FreeCAD MCP is one self-contained binary: no Python, uv or pip is needed. A computer that only connects to FreeCAD running elsewhere needs just freecad-mcp (see [remote access](docs/remote-access.md)).

## Install

Windows (PowerShell):

```powershell
irm https://github.com/sairaph/freecad-mcp/releases/latest/download/install.ps1 | iex
```

macOS / Linux:

```sh
curl -fsSL https://github.com/sairaph/freecad-mcp/releases/latest/download/install.sh | sh
```

The installer downloads the binary, checks its SHA256 checksum and starts a setup wizard. The wizard finds the AI clients on your machine and registers FreeCAD MCP with the ones you pick, installs the addon into FreeCAD, and installs the guide skill. Nothing is written until you confirm, so cancelling earlier changes nothing. See the [installation guide](docs/installation.md) for the steps, unattended installs, manual setup and troubleshooting.

Restart FreeCAD and your AI client, then ask for a model. Your client can start FreeCAD itself with the `start_freecad` tool. Run `freecad-mcp doctor` to check the installation and `freecad-mcp update` to update the server and its addon (restart FreeCAD afterwards).

## Security

Connections use `localhost` by default. Without a password, any program on your machine can call FreeCAD's RPC server: set a password, or let other computers connect, as described in [remote access](docs/remote-access.md).

## Documentation

| Guide | Contents |
| --- | --- |
| [Installation](docs/installation.md) | Installer, commands, addon directories, running from source |
| [Configuration](docs/configuration.md) | Environment variables, auto-start, text feedback, remote access |
| [Tools](docs/tools.md) | Available tools, screenshots, file import/export, printability and mesh checks, FEM analysis |
| [Code execution](docs/execution.md) | GUI execution, background jobs, headless scripts, timeout troubleshooting |
| [Remote access](docs/remote-access.md) | Sharing FreeCAD with other devices, the listener, security, SSH tunnel, multi-agent rules |
| [Example scripts](docs/examples.md) | FEM script, ADK and LangChain agent examples |

## Credits

This project was forked and reworked from [neka-nat/freecad-mcp](https://github.com/neka-nat/freecad-mcp).
