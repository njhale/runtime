package helper

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"text/template"
)

// ForkDir creates a temporary copy of a directory tree and returns its path.
// Each copy is tied to the lifetime of the test that created it, and is deleted when that test is over.
func ForkDir(t *testing.T, path string) (forkPath string) {
	t.Helper()

	// Create a temporary directory to hold the copies
	pattern := fmt.Sprintf("%s.*.%s", t.Name(), filepath.Base(path))
	forkPath = MustBe(os.MkdirTemp("", pattern))

	// Be sure to delete the copy once the test that requested it is done
	t.Cleanup(func() {
		Must(os.Remove(forkPath))
	})

	copyFile := func(src, dest string) {
		srcFile, destFile := MustBe(os.Open(src)), MustBe(os.Create(dest))
		defer func() { Must(srcFile.Close(), destFile.Close()) }()
		MustBe(io.Copy(destFile, srcFile))
	}

	var copyDir func(string, string)
	copyDir = func(src, dest string) {
		for _, fd := range MustBe(os.ReadDir(src)) {
			name := fd.Name()
			nextSrc, nextDest := filepath.Join(src, name), filepath.Join(dest, name)
			switch mode := fd.Type(); {
			case mode.IsDir():
				// Traverse subtree
				copyDir(nextSrc, nextDest)
			case mode.IsRegular():
				// Copy regular file
				copyFile(nextSrc, nextDest)
			}
			// Ignore links and other file types
		}
	}

	copyDir(path, forkPath)

	return forkPath
}

// InflateTemplate overwrites a Go template file with the result of executing it against the given data.
func InflateTemplate(t *testing.T, path string, data any) {
	t.Helper()

	// Create and parse the template
	parsed := MustBe(template.ParseFiles(path))

	// Open the result
	resultFile := MustBe(os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644))
	Must(parsed.Execute(resultFile, data))
}
