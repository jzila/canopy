package sandbox

import (
	"testing"
)

func TestExtractFilesFromPatches(t *testing.T) {
	tests := []struct {
		name     string
		patches  []string
		expected []string
	}{
		{
			name:     "empty patches",
			patches:  []string{},
			expected: nil,
		},
		{
			name: "single file patch",
			patches: []string{
				`From abc123 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Subject: [PATCH] Add feature

---
diff --git a/pkg/foo/bar.go b/pkg/foo/bar.go
index 1234567..abcdefg 100644
--- a/pkg/foo/bar.go
+++ b/pkg/foo/bar.go
@@ -1,3 +1,4 @@
 package foo
+// new line
`,
			},
			expected: []string{"pkg/foo/bar.go"},
		},
		{
			name: "multiple files in single patch",
			patches: []string{
				`From abc123 Mon Sep 17 00:00:00 2001
From: Test User <test@example.com>
Subject: [PATCH] Multiple changes

---
diff --git a/cmd/main.go b/cmd/main.go
index 1234567..abcdefg 100644
--- a/cmd/main.go
+++ b/cmd/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
diff --git a/pkg/util/helper.go b/pkg/util/helper.go
index 1234567..abcdefg 100644
--- a/pkg/util/helper.go
+++ b/pkg/util/helper.go
@@ -1,3 +1,4 @@
 package util
+// comment
`,
			},
			expected: []string{"cmd/main.go", "pkg/util/helper.go"},
		},
		{
			name: "multiple patches",
			patches: []string{
				`diff --git a/file1.go b/file1.go
--- a/file1.go
+++ b/file1.go
`,
				`diff --git a/file2.go b/file2.go
--- a/file2.go
+++ b/file2.go
`,
			},
			expected: []string{"file1.go", "file2.go"},
		},
		{
			name: "same file in multiple patches (deduplication)",
			patches: []string{
				`diff --git a/shared.go b/shared.go
--- a/shared.go
+++ b/shared.go
`,
				`diff --git a/shared.go b/shared.go
--- a/shared.go
+++ b/shared.go
`,
			},
			expected: []string{"shared.go"},
		},
		{
			name: "new file (a/dev/null)",
			patches: []string{
				`diff --git a/dev/null b/newfile.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/newfile.go
`,
			},
			expected: []string{"newfile.go"},
		},
		// Note: Paths with spaces are rare in codebases and git format-patch
		// may quote them differently. The current implementation handles
		// the common case of paths without spaces.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractFilesFromPatches(tt.patches)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d files, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("file %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestExtractFilesFromPatchesWithRenames(t *testing.T) {
	// Git renames show both old and new paths, we extract the destination (b/ path)
	patches := []string{
		`diff --git a/old/path.go b/new/path.go
similarity index 95%
rename from old/path.go
rename to new/path.go
--- a/old/path.go
+++ b/new/path.go
`,
	}

	result := ExtractFilesFromPatches(patches)

	if len(result) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(result), result)
		return
	}

	// We extract the b/ path (destination)
	if result[0] != "new/path.go" {
		t.Errorf("expected 'new/path.go', got %q", result[0])
	}
}
