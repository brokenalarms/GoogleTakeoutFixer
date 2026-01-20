package fixer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Read the contents of a directory and returns a slice containing the directory entries
func ReadDirectory(path string) []os.DirEntry {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading directory %s: %v\n", path, err)
		return nil
	}
	return entries
}

// Copies a file from a source path to a destination path
func DuplicateFile(source string, destination string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// Separates year folders from album folders based on its naming
func FindYearAlbumFolders(albums []os.DirEntry) ([]os.DirEntry, []os.DirEntry) {
	// todo: add language support
	re := regexp.MustCompile(`^Photos from \d+$`)

	var yearFolders []os.DirEntry
	var albumFolders []os.DirEntry

	for _, entry := range albums {
		_, err := entry.Info()
		if err == nil {
			if re.MatchString(entry.Name()) {
				yearFolders = append(yearFolders, entry)
			} else {
				albumFolders = append(albumFolders, entry)
			}
		}
	}

	return yearFolders, albumFolders
}

// Returns a list of directories within the given path
func FindDirs(path string) []os.DirEntry {
	var dirlist []os.DirEntry

	files, err := os.ReadDir(path)

	if err != nil {
		fmt.Println("Error: " + err.Error())
		return dirlist
	}

	for _, file := range files {
		if file.IsDir() {
			dirlist = append(dirlist, file)
		}
	}

	return dirlist
}

// Checks if a sidecar file with the given suffix exists for the original file
func HasSidecarFile(originalPath string, suffix string) bool {
	sidecarPath := originalPath + suffix
	_, err := os.Stat(sidecarPath)
	return err == nil
}

// Checks if the file at the given path has the specified extension
func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}
