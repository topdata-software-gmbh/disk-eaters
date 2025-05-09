# Disk Eaters

A tool for identifying large files and directories consuming disk space, tracking disk usage growth over time, and helping manage storage efficiently.

## Features

- Scans directories to find the largest files and directories
- Tracks disk usage growth between scans
- Configurable output with customizable number of items to display
- Supports logging to specified directories
- Maintains history of scans for comparison

## Installation

### Prerequisites

- Go 1.16 or higher

### Building from source

```bash
git clone https://github.com/yourusername/disk_eaters.git
cd disk_eaters
go build disk_eaters_go.go
```

## Usage

```bash
./disk_eaters_go -dir="/path/to/scan" -log="/var/log/disk_eaters" -max=10
```

### Command-line options

- `-dir`: Directory to scan (default: "/")
- `-log`: Log directory (default: "/var/log/disk_eaters")
- `-max`: Maximum number of items to show (default: 5)

## How It Works

Disk Eaters scans the specified directory and identifies:

1. The largest directories
2. The largest files
3. The fastest growing directories/files (when comparing with previous scans)

The tool uses concurrent processing to efficiently scan large directory structures and can detect when it crosses filesystem boundaries.

## License

[MIT License](LICENSE)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request
