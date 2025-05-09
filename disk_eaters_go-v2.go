package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DiskEntry represents a file or directory with its size
type DiskEntry struct {
	Path string
	Size int64
}

// ProcessInfo represents a process using a file
type ProcessInfo struct {
	PID     string
	User    string
	Command string
	Access  string // Access mode (r, w, etc.)
}

// Config holds the application configuration
type Config struct {
	ScanDir          string
	LogDir           string
	HistoryDir       string
	CurrentSnapshot  string
	PreviousSnapshot string
	MaxItems         int
	ShowProcesses    bool
}

func main() {
	// Parse command-line arguments
	scanDir := flag.String("dir", "/", "Directory to scan")
	logDir := flag.String("log", "/var/log/disk_eaters", "Log directory")
	maxItems := flag.Int("max", 5, "Maximum number of items to show")
	showProcesses := flag.Bool("processes", true, "Show processes using the files")
	flag.Parse()

	// Create config
	config := Config{
		ScanDir:          *scanDir,
		LogDir:           *logDir,
		HistoryDir:       filepath.Join(*logDir, "history"),
		CurrentSnapshot:  filepath.Join(*logDir, "current"),
		PreviousSnapshot: filepath.Join(*logDir, "previous"),
		MaxItems:         *maxItems,
		ShowProcesses:    *showProcesses,
	}

	// Ensure log directories exist
	os.MkdirAll(config.LogDir, 0755)
	os.MkdirAll(config.HistoryDir, 0755)

	// Setup result file
	date := time.Now().Format("2006-01-02")
	resultFile := filepath.Join(config.HistoryDir, fmt.Sprintf("disk_eaters_%s.log", date))
	f, err := os.Create(resultFile)
	if err != nil {
		fmt.Printf("Error creating result file: %v\n", err)
		return
	}
	defer f.Close()

	// Create a multiwriter to write to both file and stdout
	mw := io.MultiWriter(os.Stdout, f)

	// Write header
	fmt.Fprintf(mw, "DISK EATERS WATCH REPORT - %s\n", date)
	fmt.Fprintf(mw, "Scan Directory: %s\n\n", config.ScanDir)

	// Find largest directories
	printHeader(mw, fmt.Sprintf("TOP %d LARGEST DIRECTORIES UNDER %s", config.MaxItems, config.ScanDir))
	dirs, err := findLargestDirectories(config.ScanDir, config.MaxItems)
	if err != nil {
		fmt.Fprintf(mw, "Error finding directories: %v\n", err)
	} else {
		for _, dir := range dirs {
			fmt.Fprintf(mw, "%s\t%s\n", formatSize(dir.Size), dir.Path)
		}
		// Save current directories data
		saveEntries(dirs, config.CurrentSnapshot+".dirs")
	}
	fmt.Fprintln(mw, "")

	// Find largest files
	printHeader(mw, fmt.Sprintf("TOP %d LARGEST FILES UNDER %s", config.MaxItems, config.ScanDir))
	files, err := findLargestFiles(config.ScanDir, config.MaxItems)
	if err != nil {
		fmt.Fprintf(mw, "Error finding files: %v\n", err)
	} else {
		for _, file := range files {
			fmt.Fprintf(mw, "%s\t%s\n", formatSize(file.Size), file.Path)
		}

		// Show processes using these files if requested
		if config.ShowProcesses && len(files) > 0 {
			fmt.Fprintln(mw, "")
			printHeader(mw, "PROCESSES USING LARGE FILES")
			
			for _, file := range files {
				fmt.Fprintf(mw, "\nFile: %s (%s)\n", file.Path, formatSize(file.Size))
				
				processes, err := findProcessesUsingFile(file.Path)
				if err != nil {
					fmt.Fprintf(mw, "  Error finding processes: %v\n", err)
					continue
				}
				
				if len(processes) == 0 {
					fmt.Fprintf(mw, "  No processes currently using this file\n")
				} else {
					fmt.Fprintf(mw, "  %-8s %-10s %-8s %s\n", "PID", "USER", "ACCESS", "COMMAND")
					fmt.Fprintf(mw, "  %-8s %-10s %-8s %s\n", "---", "----", "------", "-------")
					for _, proc := range processes {
						fmt.Fprintf(mw, "  %-8s %-10s %-8s %s\n", proc.PID, proc.User, proc.Access, proc.Command)
					}
				}
			}
		}
		
		// Save current files data
		saveEntries(files, config.CurrentSnapshot+".files")
	}
	fmt.Fprintln(mw, "")

	// Analyze growth if previous data exists
	printHeader(mw, fmt.Sprintf("TOP %d FASTEST GROWING DIRECTORIES UNDER %s", config.MaxItems, config.ScanDir))
	if _, err := os.Stat(config.PreviousSnapshot + ".dirs"); err == nil {
		dirGrowth, err := analyzeGrowth(config.CurrentSnapshot+".dirs", config.PreviousSnapshot+".dirs", config.MaxItems)
		if err != nil {
			fmt.Fprintf(mw, "Error analyzing directory growth: %v\n", err)
		} else {
			for _, growth := range dirGrowth {
				fmt.Fprintf(mw, "%s\t%s\n", formatSize(growth.Size), growth.Path)
			}
		}
	} else {
		fmt.Fprintln(mw, "No previous data available for comparison. Growth analysis will be available after the next run.")
	}
	fmt.Fprintln(mw, "")

	printHeader(mw, fmt.Sprintf("TOP %d FASTEST GROWING FILES UNDER %s", config.MaxItems, config.ScanDir))
	if _, err := os.Stat(config.PreviousSnapshot + ".files"); err == nil {
		fileGrowth, err := analyzeGrowth(config.CurrentSnapshot+".files", config.PreviousSnapshot+".files", config.MaxItems)
		if err != nil {
			fmt.Fprintf(mw, "Error analyzing file growth: %v\n", err)
		} else {
			for _, growth := range fileGrowth {
				fmt.Fprintf(mw, "%s\t%s\n", formatSize(growth.Size), growth.Path)
			}
			
			// Show processes using these growing files if requested
			if config.ShowProcesses && len(fileGrowth) > 0 {
				fmt.Fprintln(mw, "")
				printHeader(mw, "PROCESSES USING FAST-GROWING FILES")
				
				for _, file := range fileGrowth {
					fmt.Fprintf(mw, "\nFile: %s (grew by %s)\n", file.Path, formatSize(file.Size))
					
					processes, err := findProcessesUsingFile(file.Path)
					if err != nil {
						fmt.Fprintf(mw, "  Error finding processes: %v\n", err)
						continue
					}
					
					if len(processes) == 0 {
						fmt.Fprintf(mw, "  No processes currently using this file\n")
					} else {
						fmt.Fprintf(mw, "  %-8s %-10s %-8s %s\n", "PID", "USER", "ACCESS", "COMMAND")
						fmt.Fprintf(mw, "  %-8s %-10s %-8s %s\n", "---", "----", "------", "-------")
						for _, proc := range processes {
							fmt.Fprintf(mw, "  %-8s %-10s %-8s %s\n", proc.PID, proc.User, proc.Access, proc.Command)
						}
					}
				}
			}
		}
	} else {
		fmt.Fprintln(mw, "No previous data available for comparison. Growth analysis will be available after the next run.")
	}
	fmt.Fprintln(mw, "")

	// Archive current data for next run's comparison
	if _, err := os.Stat(config.CurrentSnapshot + ".dirs"); err == nil {
		copyFile(config.CurrentSnapshot+".dirs", config.PreviousSnapshot+".dirs")
	}
	if _, err := os.Stat(config.CurrentSnapshot + ".files"); err == nil {
		copyFile(config.CurrentSnapshot+".files", config.PreviousSnapshot+".files")
	}

	// Print summary
	printHeader(mw, "SUMMARY")
	fmt.Fprintf(mw, "Log saved to: %s\n", resultFile)
	fmt.Fprintln(mw, "Run this program daily to track growth patterns.")
	fmt.Fprintln(mw, "")

	// Add cron setup instructions
	fmt.Fprintln(mw, "--------------------------------------------------------")
	fmt.Fprintln(mw, "CRON SETUP INSTRUCTIONS")
	fmt.Fprintln(mw, "--------------------------------------------------------")
	fmt.Fprintln(mw, "To run this program daily via cron, execute:")
	fmt.Fprintln(mw, "")
	fmt.Fprintln(mw, "sudo crontab -e")
	fmt.Fprintln(mw, "")
	fmt.Fprintln(mw, "Then add the following line:")
	fmt.Fprintln(mw, "")
	fmt.Fprintln(mw, "# Run disk eaters watch program daily at 2 AM")
	fmt.Fprintln(mw, "0 2 * * * /path/to/disk_eaters -dir / > /dev/null 2>&1")
	fmt.Fprintln(mw, "")
	fmt.Fprintln(mw, "Replace \"/path/to/\" with the actual path where you saved this program.")
	fmt.Fprintln(mw, "Replace \"/\" with the directory you want to scan if not the root.")
}

// findProcessesUsingFile finds all processes using a file
func findProcessesUsingFile(filePath string) ([]ProcessInfo, error) {
	var processes []ProcessInfo
	
	// Different implementations for different operating systems
	switch runtime.GOOS {
	case "linux":
		return findProcessesLinux(filePath)
	case "darwin":
		return findProcessesMacOS(filePath)
	case "windows":
		return findProcessesWindows(filePath)
	default:
		return nil, fmt.Errorf("process finding not implemented for %s", runtime.GOOS)
	}

	return processes, nil
}

// findProcessesLinux finds processes using a file on Linux using lsof
func findProcessesLinux(filePath string) ([]ProcessInfo, error) {
	var processes []ProcessInfo

	// Try to use lsof first
	cmd := exec.Command("lsof", "-F", "pcun", filePath)
	output, err := cmd.Output()
	if err == nil {
		// Parse lsof output
		return parseLsofOutput(string(output)), nil
	}
	
	// Fall back to fuser if lsof fails
	cmd = exec.Command("fuser", "-v", filePath)
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) >= 2 {
			// Skip the header line and process the rest
			for i := 1; i < len(lines); i++ {
				if lines[i] == "" {
					continue
				}
				
				fields := strings.Fields(lines[i])
				if len(fields) >= 3 {
					pid := fields[0]
					user := fields[1]
					access := "?"
					command := strings.Join(fields[2:], " ")
					
					processes = append(processes, ProcessInfo{
						PID:     pid,
						User:    user,
						Command: command,
						Access:  access,
					})
				}
			}
		}
		return processes, nil
	}
	
	// If both fail, try to use /proc directly (Linux-specific)
	files, err := filepath.Glob("/proc/[0-9]*/fd/*")
	if err != nil {
		return nil, err
	}
	
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	
	for _, fdPath := range files {
		target, err := os.Readlink(fdPath)
		if err == nil && target == absPath {
			// Extract PID from path
			parts := strings.Split(fdPath, "/")
			if len(parts) >= 3 {
				pid := parts[2]
				
				// Get user and command
				cmdlinePath := filepath.Join("/proc", pid, "cmdline")
				cmdline, err := os.ReadFile(cmdlinePath)
				if err != nil {
					continue
				}
				
				// Replace null bytes with spaces
				command := strings.ReplaceAll(string(cmdline), "\x00", " ")
				
				// Get user if possible
				statusPath := filepath.Join("/proc", pid, "status")
				statusContent, err := os.ReadFile(statusPath)
				if err != nil {
					continue
				}
				
				user := "?"
				for _, line := range strings.Split(string(statusContent), "\n") {
					if strings.HasPrefix(line, "Uid:") {
						fields := strings.Fields(line)
						if len(fields) >= 2 {
							user = fields[1]
							break
						}
					}
				}
				
				// Check file access mode
				var access string
				if strings.Contains(fdPath, "r") {
					access = "r"
				} else if strings.Contains(fdPath, "w") {
					access = "w"
				} else {
					access = "?"
				}
				
				processes = append(processes, ProcessInfo{
					PID:     pid,
					User:    user,
					Command: command,
					Access:  access,
				})
			}
		}
	}
	
	return processes, nil
}

// findProcessesMacOS finds processes using a file on macOS
func findProcessesMacOS(filePath string) ([]ProcessInfo, error) {
	// macOS uses lsof
	cmd := exec.Command("lsof", "-F", "pcun", filePath)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	return parseLsofOutput(string(output)), nil
}

// findProcessesWindows finds processes using a file on Windows
func findProcessesWindows(filePath string) ([]ProcessInfo, error) {
	// On Windows, we can use handle.exe from Sysinternals if available
	cmd := exec.Command("handle", filePath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("process finding on Windows requires Sysinternals Handle tool")
	}
	
	processes := []ProcessInfo{}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, filePath) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				pidMatch := regexp.MustCompile(`pid: (\d+)`).FindStringSubmatch(line)
				if len(pidMatch) >= 2 {
					pid := pidMatch[1]
					
					// Try to get more info about this process
					cmdProc := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %s", pid), "/FO", "CSV")
					procOutput, err := cmdProc.Output()
					if err == nil {
						procLines := strings.Split(string(procOutput), "\n")
						if len(procLines) >= 2 {
							// Parse CSV output
							csvFields := strings.Split(procLines[1], ",")
							if len(csvFields) >= 2 {
								command := strings.Trim(csvFields[0], "\"")
								
								processes = append(processes, ProcessInfo{
									PID:     pid,
									User:    "N/A", // Windows doesn't easily show this in tasklist
									Command: command,
									Access:  "?",
								})
							}
						}
					}
				}
			}
		}
	}
	
	return processes, nil
}

// parseLsofOutput parses the output of lsof -F pcun
func parseLsofOutput(output string) []ProcessInfo {
	var processes []ProcessInfo
	var currentProcess ProcessInfo
	
	// lsof -F output format has one character field identifiers
	// p: PID, c: command, u: user, n: filename
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		
		fieldType := line[0]
		value := line[1:]
		
		switch fieldType {
		case 'p':
			// Start of a new process
			if currentProcess.PID != "" {
				processes = append(processes, currentProcess)
			}
			currentProcess = ProcessInfo{PID: value, Access: "?"}
		case 'c':
			currentProcess.Command = value
		case 'u':
			currentProcess.User = value
		case 'f':
			// File descriptor mode
			if strings.Contains(value, "r") {
				currentProcess.Access = "r"
			} else if strings.Contains(value, "w") {
				currentProcess.Access = "w"
			} else if strings.Contains(value, "u") {
				currentProcess.Access = "rw"
			}
		}
	}
	
	// Add the last process if any
	if currentProcess.PID != "" {
		processes = append(processes, currentProcess)
	}
	
	return processes
}

// printHeader prints a formatted header to the given writer
func printHeader(w io.Writer, header string) {
	fmt.Fprintln(w, "==================================================")
	fmt.Fprintf(w, "  %s\n", header)
	fmt.Fprintln(w, "==================================================")
}

// formatSize converts size in bytes to human-readable format
func formatSize(sizeInBytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case sizeInBytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(sizeInBytes)/float64(TB))
	case sizeInBytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(sizeInBytes)/float64(GB))
	case sizeInBytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(sizeInBytes)/float64(MB))
	case sizeInBytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(sizeInBytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", sizeInBytes)
	}
}

// findLargestDirectories finds the largest directories in the given path
func findLargestDirectories(rootPath string, maxItems int) ([]DiskEntry, error) {
	var allDirs []DiskEntry
	var mutex sync.Mutex
	var wg sync.WaitGroup

	// Process a directory and its immediate subdirectories
	var processDir func(path string, depth int)
	processDir = func(path string, depth int) {
		defer wg.Done()

		// Don't go too deep
		if depth > 4 {
			return
		}

		var dirSize int64
		var subDirs []string

		// Walk the directory
		filepath.Walk(path, func(subPath string, info os.FileInfo, err error) error {
			// Skip errors
			if err != nil {
				return filepath.SkipDir
			}

			// Skip different filesystems
			if subPath != path && info.IsDir() && isOnDifferentFilesystem(path, subPath) {
				return filepath.SkipDir
			}

			// Add file sizes
			if !info.IsDir() {
				dirSize += info.Size()
				return nil
			}

			// Skip processing current dir
			if subPath == path {
				return nil
			}

			// Collect subdirectories for concurrent processing
			if depth < 4 && filepath.Dir(subPath) == path {
				subDirs = append(subDirs, subPath)
				return filepath.SkipDir // Skip further traversal of this subdir for now
			}

			return nil
		})

		// Add this directory to our list
		mutex.Lock()
		allDirs = append(allDirs, DiskEntry{Path: path, Size: dirSize})
		mutex.Unlock()

		// Process subdirectories concurrently
		for _, subDir := range subDirs {
			wg.Add(1)
			go processDir(subDir, depth+1)
		}
	}

	// Start processing the root directory
	wg.Add(1)
	processDir(rootPath, 0)
	wg.Wait()

	// Sort by size (largest first)
	sort.Slice(allDirs, func(i, j int) bool {
		return allDirs[i].Size > allDirs[j].Size
	})

	// Return top N
	if len(allDirs) > maxItems {
		return allDirs[:maxItems], nil
	}
	return allDirs, nil
}

// findLargestFiles finds the largest files in the given path
func findLargestFiles(rootPath string, maxItems int) ([]DiskEntry, error) {
	var allFiles []DiskEntry
	var mutex sync.Mutex

	// Walk all files
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		// Skip errors
		if err != nil {
			return nil
		}

		// Skip directories and symbolic links
		if info.IsDir() {
			if isOnDifferentFilesystem(rootPath, path) && path != rootPath {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		mutex.Lock()
		allFiles = append(allFiles, DiskEntry{Path: path, Size: info.Size()})
		mutex.Unlock()

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort by size (largest first)
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Size > allFiles[j].Size
	})

	// Return top N
	if len(allFiles) > maxItems {
		return allFiles[:maxItems], nil
	}
	return allFiles, nil
}

// isOnDifferentFilesystem checks if two paths are on different filesystems
func isOnDifferentFilesystem(path1, path2 string) bool {
	stat1, err1 := os.Stat(path1)
	stat2, err2 := os.Stat(path2)
	if err1 != nil || err2 != nil {
		return false
	}

	return stat1.Sys() != stat2.Sys()
}

// saveEntries saves disk entries to a file
func saveEntries(entries []DiskEntry, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		fmt.Fprintf(writer, "%d\t%s\n", entry.Size, entry.Path)
	}
	return writer.Flush()
}

// analyzeGrowth compares current and previous data to find growth
func analyzeGrowth(currentFile, previousFile string, maxItems int) ([]DiskEntry, error) {
	// Load current entries
	current, err := loadEntries(currentFile)
	if err != nil {
		return nil, err
	}

	// Load previous entries
	previous, err := loadEntries(previousFile)
	if err != nil {
		return nil, err
	}

	// Create map for previous entries
	prevMap := make(map[string]int64)
	for _, entry := range previous {
		prevMap[entry.Path] = entry.Size
	}

	// Calculate growth
	var growthEntries []DiskEntry
	for _, entry := range current {
		if prevSize, exists := prevMap[entry.Path]; exists {
			growth := entry.Size - prevSize
			if growth > 0 {
				growthEntries = append(growthEntries, DiskEntry{
					Path: entry.Path,
					Size: growth,
				})
			}
		}
	}

	// Sort by growth (largest first)
	sort.Slice(growthEntries, func(i, j int) bool {
		return growthEntries[i].Size > growthEntries[j].Size
	})

	// Return top N
	if len(growthEntries) > maxItems {
		return growthEntries[:maxItems], nil
	}
	return growthEntries, nil
}

// loadEntries loads disk entries from a file
func loadEntries(filename string) ([]DiskEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []DiskEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		size, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}

		entries = append(entries, DiskEntry{
			Path: parts[1],
			Size: size,
		})
	}

	return entries, scanner.Err()
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Sync()
}
