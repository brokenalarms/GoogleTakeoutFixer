package fixer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var sidecarSuffixes = []string{
	".supplemental-m.json",
	".supplemental-metadata.json",
	".supplemental-metada.json",
}

// Duplicate a file from one path to another
func DuplicateFile(inputPath string, outputPath string) error {
	sourceFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// Discover directories within a path non recursively
func DiscoverDirs(path string) ([]os.DirEntry, error) {
	var dirList []os.DirEntry

	files, err := os.ReadDir(path)

	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			dirList = append(dirList, file)
		}
	}

	return dirList, nil
}

// Find a matching sidecar JSON
func FindSidecar(imagePath string) string {
	for _, suffix := range sidecarSuffixes {
		p := imagePath + suffix
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	fmt.Printf("No sidecar file found for %s\n", imagePath)
	return ""
}

// Checks if the file at the given path has the specified extension
func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}

// Counts all image files within a directory recursively
func CountImagesRecursive(path string) (int, error) {
	count := 0

	err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		extension := strings.ToLower(filepath.Ext(d.Name()))
		if extension == ".jpg" || extension == ".jpeg" || extension == ".png" {
			count++
		}
		return nil
	})

	return count, err
}
