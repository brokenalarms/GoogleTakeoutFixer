package fixer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Struct to hold the structure of the JSON metadata
type imageMetadata struct {
	Title          string `json:"title"`
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"`
	} `json:"photoTakenTime"`
}

// Reads JSON and returns some of its metadata contents using the imageMetadata struct
func ReadJsonMetadata(jsonPath string) (imageMetadata, error) {
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

// Apply a timestamp to a file
func ApplyFileTime(filePath string, meta imageMetadata) error {
	// Parse timestamp
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	// Convert to local timezone
	newTime := time.Unix(timestampInt, 0).Local()

	// Update EXIF metadata using exiftool
	exifTime := newTime.Format("2006:01:02 15:04:05")

	args := []string{
		"-overwrite_original",
		"-DateTimeOriginal=" + exifTime,
		"-CreateDate=" + exifTime,
	}

	// If a title exists, set it
	if meta.Title != "" {
		args = append(args, "-ImageDescription="+meta.Title)
	}

	args = append(args, filePath)

	cmd := exec.Command("exiftool", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exiftool error: %v\n%s", err, output)
	}

	// Update file system timestamps after EXIF has been set
	if err := os.Chtimes(filePath, newTime, newTime); err != nil {
		return fmt.Errorf("failed to update file timestamps: %v", err)
	}

	return nil
}
