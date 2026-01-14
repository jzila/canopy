package daemon

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/jzila/canopy/pkg/logging"
)

// TailLines reads the last n lines from the log file.
// Returns an error if the log file doesn't exist or can't be read.
func TailLines(n int) error {
	logPath := logging.LogPath()

	file, err := os.Open(logPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", logPath)
	}
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Read all lines into memory (simple approach for typical log sizes)
	var lines []string
	scanner := bufio.NewScanner(file)
	// Increase buffer size for potentially long log lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	// Output the last n lines
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}

	return nil
}

// TailFollow continuously reads new lines from the log file and prints them.
// This function blocks until an error occurs or the process is interrupted.
func TailFollow(lines int) error {
	logPath := logging.LogPath()

	file, err := os.Open(logPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", logPath)
	}
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// First, show the last N lines
	if lines > 0 {
		if err := printLastLines(file, lines); err != nil {
			return err
		}
	}

	// Seek to end of file to start following
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek to end of file: %w", err)
	}

	// Continuously read new content
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			// No new content, wait a bit before trying again
			// Using a simple sleep loop for portability
			continue
		}
		if err != nil {
			return fmt.Errorf("error reading log file: %w", err)
		}
		// Print without newline since ReadString includes it
		fmt.Print(line)
	}
}

// printLastLines prints the last n lines from the current position in the file.
func printLastLines(file *os.File, n int) error {
	// Read from start
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to start: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	// Output the last n lines
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}

	return nil
}
