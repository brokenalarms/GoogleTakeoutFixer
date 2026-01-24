package fixer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func Process(sourcePath string, outputPath string) error {
	dirs, err := DiscoverDirs(sourcePath)
	if err != nil {
		fmt.Println("error discovering: ", err)
	}

	fmt.Println(dirs)

	err = ProcessFile(sourcePath, outputPath)
	if err != nil {
		fmt.Println(err)
	}

	for _, dir := range dirs {

		dirPath := string(sourcePath) + /*string(os.PathSeparator) + */ dir.Name()
		fmt.Println(dirPath)

		var targetPath string = outputPath + dir.Name()

		ProcessDirectory(dirPath, targetPath)

		isYear, err := CheckWhetherYear(dir.Name())

		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(dir.Name(), ":", isYear)
	}

	return nil
}

func CheckWhetherYear(dirPath string) (bool, error) {
	re := regexp.MustCompile(`^Photos from \d+$`)

	if re.MatchString(dirPath) {
		return true, nil
	} else {
		return false, nil
	}
}

func ProcessDirectory(dirPath string, outputPath string) error {
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
		//fmt.Println(imagePath)

		// check whether file is a image file
		if !IsNameExtension(".jpg", imagePath) && !IsNameExtension(".png", imagePath) {
			continue
		}

		ProcessFile(imagePath, outputPath)

	}

	return nil
}

func ProcessFile(sourcePath string, outputPath string) error {
	sidecarPath := FindSidecar(sourcePath)

	// Metadata sidecar file not found
	if sidecarPath == "" {
		return nil
	}

	fmt.Println(sidecarPath)

	meta, err := ReadJsonMeta(sidecarPath)
	if err != nil {
		fmt.Println("error reading metadata: ", err)
	}

	CreateFixedFile(sourcePath, sidecarPath, outputPath)
	fmt.Println(sourcePath, sidecarPath, outputPath)

	fmt.Println(meta.PhotoTakenTime)

	return nil
}

func CreateFixedFile(filePath string, fileMetadataPath string, outputPath string) error {
	// ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}

	fileName := filepath.Base(filePath)
	destPath := filepath.Join(outputPath, fileName)

	if err := CopyFile(filePath, destPath); err != nil {
		return err
	}

	metadata, err := ReadJsonMeta(fileMetadataPath)
	if err != nil {
		return err
	}

	ApplyFileTime(destPath, metadata)

	return nil
}

func CopyFile(inputPath string, outputPath string) error {
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

func ReadJsonMeta(jsonPath string) (imageMetadata, error) {
	var data imageMetadata

	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return data, err
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return data, err
	}

	err = json.Unmarshal(byteValue, &data)
	return data, err
}

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

var sidecarSuffixes = []string{
	".supplemental-m.json",
	".supplemental-metadata.json",
	".supplemental-metada.json",
}

func FindSidecar(imagePath string) string {
	for _, suffix := range sidecarSuffixes {
		p := imagePath + suffix
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Checks if the file at the given path has the specified extension
func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}
