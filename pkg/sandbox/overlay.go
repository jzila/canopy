package sandbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileChange represents a modification in the overlay
type FileChange struct {
	Path    string
	Type    ChangeType
	NewHash string
}

// ChangeType indicates the type of file change
type ChangeType string

const (
	ChangeCreated  ChangeType = "created"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
)

// Overlay represents an OverlayFS mount for agent isolation
type Overlay struct {
	ID         string
	LowerDir   string   // Read-only base (original working directory)
	UpperDir   string   // Writable layer (per-agent changes)
	WorkDir    string   // OverlayFS internal workdir
	MergedDir  string   // Combined view where agent operates
	mounted    bool
	useFuse    bool
	bindMounts []string // Paths that are bind-mounted through the overlay
}

// StaleMountInfo contains information about a stale overlay mount
type StaleMountInfo struct {
	MergedDir string // Full path to the merged directory mount point
	ID        string // Overlay ID (directory name under baseDir)
	IsFuse    bool   // True if this is a FUSE mount (vs kernel overlay)
}

// DefaultPassthroughPaths are directories that should bypass the overlay
// and write directly to the original filesystem
var DefaultPassthroughPaths = []string{
	// Empty - .beads removed to prevent git conflicts from concurrent workers
}

// DefaultHiddenPaths are paths that should be hidden from agents via whiteout
// These won't be visible in the merged view even if they exist in lowerdir
var DefaultHiddenPaths = []string{
	".claude", // Hide parent session settings and approved commands
}

// HomeExcludedPaths are paths created by tools when HOME is set to merged dir
// These should be excluded from GetChanges() as they're not project files
var HomeExcludedPaths = []string{
	".npm",          // npm cache and global packages
	".cache",        // General cache directory
	".claude.json",  // Claude CLI credentials (copied to overlay)
	".gitconfig",    // Git config (copied to overlay)
	".config",       // General config directory (may contain tool state)
	".local",        // User-local data directory
	".bash_history", // Shell history
	".viminfo",      // Vim state
	".lesshst",      // Less history
}

// NewOverlay creates a new overlay filesystem structure
func NewOverlay(baseDir, lowerDir string) (*Overlay, error) {
	id := generateID()

	overlay := &Overlay{
		ID:        id,
		LowerDir:  lowerDir,
		UpperDir:  filepath.Join(baseDir, id, "upper"),
		WorkDir:   filepath.Join(baseDir, id, "work"),
		MergedDir: filepath.Join(baseDir, id, "merged"),
	}

	// Create directory structure
	for _, dir := range []string{overlay.UpperDir, overlay.WorkDir, overlay.MergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			overlay.Cleanup()
			return nil, fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	// Create whiteouts for hidden paths
	if err := overlay.createWhiteouts(); err != nil {
		overlay.Cleanup()
		return nil, fmt.Errorf("failed to create whiteouts: %w", err)
	}

	// Copy Claude credentials to upper dir so agents can authenticate
	if err := overlay.copyClaudeCredentials(); err != nil {
		// Non-fatal - agent might work with env vars
		_ = err
	}

	// Copy git config so agents have correct authorship
	if err := overlay.copyGitConfig(); err != nil {
		// Non-fatal - agents can still commit with repo-local config
		_ = err
	}

	return overlay, nil
}

// createWhiteouts creates whiteout entries in the upper directory to hide
// sensitive paths from agents. Uses OverlayFS whiteout convention.
func (o *Overlay) createWhiteouts() error {
	for _, hiddenPath := range DefaultHiddenPaths {
		lowerPath := filepath.Join(o.LowerDir, hiddenPath)

		// Only create whiteout if path exists in lower
		info, err := os.Stat(lowerPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", hiddenPath, err)
		}

		if info.IsDir() {
			// For directories: create opaque directory with .wh..wh..opq marker
			upperDir := filepath.Join(o.UpperDir, hiddenPath)
			if err := os.MkdirAll(upperDir, 0755); err != nil {
				return fmt.Errorf("create upper dir %s: %w", hiddenPath, err)
			}
			opaquePath := filepath.Join(upperDir, ".wh..wh..opq")
			if err := os.WriteFile(opaquePath, nil, 0644); err != nil {
				return fmt.Errorf("create opaque marker for %s: %w", hiddenPath, err)
			}
		} else {
			// For files: create .wh.<filename> whiteout
			dir := filepath.Dir(hiddenPath)
			if dir != "." {
				if err := os.MkdirAll(filepath.Join(o.UpperDir, dir), 0755); err != nil {
					return fmt.Errorf("create parent dir for whiteout: %w", err)
				}
			}
			whiteoutPath := filepath.Join(o.UpperDir, dir, ".wh."+filepath.Base(hiddenPath))
			if err := os.WriteFile(whiteoutPath, nil, 0644); err != nil {
				return fmt.Errorf("create whiteout for %s: %w", hiddenPath, err)
			}
		}
	}
	return nil
}

// copyClaudeCredentials copies Claude CLI credentials from the user's home
// directory to the overlay's upper directory so agents can authenticate.
// This is necessary because .claude is whiteout'd from the lowerdir.
func (o *Overlay) copyClaudeCredentials() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get user home dir: %w", err)
	}

	// Claude credentials can be in multiple locations
	credentialPaths := []string{
		".claude.json",
		".claude/.credentials.json",
		".claude/settings.json",
	}

	claudeDir := filepath.Join(o.UpperDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("create .claude dir in upper: %w", err)
	}

	// Copy each credential file if it exists
	for _, credPath := range credentialPaths {
		srcPath := filepath.Join(homeDir, credPath)
		dstPath := filepath.Join(o.UpperDir, credPath)

		// Check if source file exists
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		// Create parent directory in upper if needed
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", credPath, err)
		}

		// Copy the file
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", credPath, err)
		}
	}

	// Also copy the entire statsig directory if it exists
	statsigSrc := filepath.Join(homeDir, ".claude", "statsig")
	statsigDst := filepath.Join(claudeDir, "statsig")
	if _, err := os.Stat(statsigSrc); err == nil {
		if err := copyDir(statsigSrc, statsigDst); err != nil {
			// Non-fatal, statsig is just for telemetry
			_ = err
		}
	}

	return nil
}

// copyGitConfig creates a minimal .gitconfig with only user.name and user.email
// for commit authorship. Does NOT copy credential helpers or URL rewrites to
// prevent agents from pushing to remote repositories.
func (o *Overlay) copyGitConfig() error {
	// Get user.name and user.email from git config
	userName, err := exec.Command("git", "config", "--global", "user.name").Output()
	if err != nil {
		return fmt.Errorf("get user.name: %w", err)
	}

	userEmail, err := exec.Command("git", "config", "--global", "user.email").Output()
	if err != nil {
		return fmt.Errorf("get user.email: %w", err)
	}

	// Trim whitespace from values
	name := strings.TrimSpace(string(userName))
	email := strings.TrimSpace(string(userEmail))

	if name == "" || email == "" {
		return fmt.Errorf("git user.name or user.email not configured")
	}

	// Create minimal gitconfig with only [user] section and SSH blocking
	// core.sshCommand = false prevents git from using SSH for remote operations
	// This blocks pushes even when SSH keys are accessible via getpwuid() home directory
	// Using "false" works because git uses shell to execute the command
	gitconfig := fmt.Sprintf("[user]\n\tname = %s\n\temail = %s\n[core]\n\tsshCommand = false\n", name, email)

	dstPath := filepath.Join(o.UpperDir, ".gitconfig")
	return os.WriteFile(dstPath, []byte(gitconfig), 0644)
}

// CopyConfigPaths copies additional config files to the overlay based on sandbox config.
// Each config file is copied to the same relative path in the upper directory.
// This allows agents to use these configs while protecting the originals.
func (o *Overlay) CopyConfigPaths(configPaths []string) error {
	for _, srcPath := range configPaths {
		// Skip if source doesn't exist
		info, err := os.Stat(srcPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", srcPath, err)
		}

		// Determine relative path from home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get user home dir: %w", err)
		}

		var relPath string
		if strings.HasPrefix(srcPath, homeDir+"/") {
			relPath = srcPath[len(homeDir)+1:]
		} else {
			// For absolute paths outside home, use basename
			relPath = filepath.Base(srcPath)
		}

		dstPath := filepath.Join(o.UpperDir, relPath)

		// Create parent directory in upper if needed
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", relPath, err)
		}

		// Copy the file or directory
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy dir %s: %w", srcPath, err)
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy file %s: %w", srcPath, err)
			}
		}
	}
	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get source file mode
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from src
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

// generateID creates a random hex ID
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GetChanges extracts all file changes from the overlay's upper directory
func (o *Overlay) GetChanges() ([]FileChange, error) {
	var changes []FileChange

	err := filepath.WalkDir(o.UpperDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory
		if path == o.UpperDir {
			return nil
		}

		// Get relative path
		relPath, _ := filepath.Rel(o.UpperDir, path)

		// Skip .git directory entirely - git changes are handled separately via commit extraction
		if relPath == ".git" || strings.HasPrefix(relPath, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip passthrough paths (they're bind-mounted, not overlayed)
		for _, passthrough := range DefaultPassthroughPaths {
			if relPath == passthrough || strings.HasPrefix(relPath, passthrough+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden paths (they're whiteouts we created, not real changes)
		for _, hidden := range DefaultHiddenPaths {
			if relPath == hidden || strings.HasPrefix(relPath, hidden+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip HOME-related paths (created when HOME=merged, not project files)
		for _, excluded := range HomeExcludedPaths {
			if relPath == excluded || strings.HasPrefix(relPath, excluded+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Check for whiteout files (deleted files in overlay)
		// Whiteout files have names prefixed with .wh.
		if strings.HasPrefix(d.Name(), ".wh.") {
			originalName := strings.TrimPrefix(d.Name(), ".wh.")
			changes = append(changes, FileChange{
				Path: filepath.Join(filepath.Dir(relPath), originalName),
				Type: ChangeDeleted,
			})
			return nil
		}

		// Skip directories (we track file contents)
		if d.IsDir() {
			return nil
		}

		// Check if file exists in lower (original)
		lowerPath := filepath.Join(o.LowerDir, relPath)
		_, lowerErr := os.Stat(lowerPath)

		change := FileChange{Path: relPath}

		if os.IsNotExist(lowerErr) {
			change.Type = ChangeCreated
		} else {
			change.Type = ChangeModified
		}

		change.NewHash, _ = hashFile(path)
		changes = append(changes, change)

		return nil
	})

	return changes, err
}

// hashFile computes SHA256 hash of a file
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// Cleanup removes all overlay directories
func (o *Overlay) Cleanup() error {
	// Always try to unmount defensively - the o.mounted flag might be stale
	// (e.g., if process crashed and was restarted, or state is inconsistent)
	// Check actual mount status and attempt unmount if needed
	if o.isMounted() {
		// Force the mounted flag to true so Unmount() will actually run
		o.mounted = true
		if err := o.Unmount(); err != nil {
			return fmt.Errorf("failed to unmount before cleanup: %w", err)
		}
	} else if o.mounted {
		// Flag says mounted but it's not - just clear the flag
		o.mounted = false
	}

	// Final verification: ensure it's really unmounted before RemoveAll
	// RemoveAll() will fail if FUSE is still mounted
	if o.isMounted() {
		return fmt.Errorf("cannot cleanup: %s is still mounted after unmount attempt", o.MergedDir)
	}

	// Remove the overlay directory tree
	baseDir := filepath.Dir(o.UpperDir)
	return os.RemoveAll(baseDir)
}

// IsMounted returns whether the overlay is currently mounted
func (o *Overlay) IsMounted() bool {
	return o.mounted
}

// UpperPath returns the full path to a file in the upper directory
func (o *Overlay) UpperPath(relPath string) string {
	return filepath.Join(o.UpperDir, relPath)
}
