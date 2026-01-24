package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Progress struct {
	Total     int
	Processed int
	Current   string
}

// Process is the main fixer entry point.
// Process
// -> DiscoverDirs
// --> ProcessDirectory
// ---> ProcessFile

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

func Process(
	sourcePath string,
	outputPath string,
	progressCh chan<- Progress,
) error {
	defer close(progressCh)
	p := Progress{}

	amountImages, err := CountImagesRecursive(sourcePath)
	p.Total = amountImages
	progressCh <- p

	dirs, err := DiscoverDirs(sourcePath)
	if err != nil {
		fmt.Println("error discovering: ", err)
	}

	err = ProcessFile(sourcePath, outputPath)
	if err != nil {
		fmt.Println(err)
	}

	for _, dir := range dirs {

		dirPath := string(sourcePath) + dir.Name()

		var targetPath string = outputPath + dir.Name()

		ProcessDirectory(dirPath, targetPath, &p, progressCh)
	}

	return nil
}

// Process a directory and fix all files within the directory. Ignores sub-directories.
func ProcessDirectory(
	dirPath string,
	outputPath string,
	p *Progress,
	progressCh chan<- Progress,
) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		imagePath := filepath.Join(dirPath, file.Name())

		if file.IsDir() {
			fmt.Println("file is a dir")
			continue
		}

		// Check whether a file is an image or not
		// TODO: Add support for any image/video file without hard coding
		// TODO: This check also happens within CountImagesRecursively, turn this into a function
		if !IsNameExtension(".jpg", imagePath) && !IsNameExtension(".png", imagePath) {
			continue
		}

		ProcessFile(imagePath, outputPath)

		p.Processed++
		p.Current = imagePath
		progressCh <- *p
	}
	return nil
}

// ProcessFile processes a single file by finding its sidecar file and then fixing it using the sidecar's metadata
func ProcessFile(sourcePath string, outputPath string) error {
	sidecarPath := FindSidecar(sourcePath)

	// Metadata sidecar file not found
	if sidecarPath == "" {
		return nil
	}

	_, err := ReadJsonMetadata(sidecarPath)
	if err != nil {
		fmt.Println("error reading metadata: ", err)
	}

	CreateFixedFile(sourcePath, sidecarPath, outputPath)

	return nil
}

// Create a fixed file with fixed metadata
func CreateFixedFile(filePath string, fileMetadataPath string, outputPath string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}

	fileName := filepath.Base(filePath)
	destPath := filepath.Join(outputPath, fileName)

	if err := DudplicateFile(filePath, destPath); err != nil {
		return err
	}

	metadata, err := ReadJsonMetadata(fileMetadataPath)
	if err != nil {
		return err
	}

	ApplyFileTime(destPath, metadata)

	return nil
}

// Checks whether a directory is a standart google year folder
func CheckWhetherYear(dirPath string) (bool, error) {
	// TODO: Add support for non english takeouts
	re := regexp.MustCompile(`^Photos from \d+$`)

	if re.MatchString(dirPath) {
		return true, nil
	} else {
		return false, nil
	}
}
