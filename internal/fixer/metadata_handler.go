package fixer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	newTime := time.Unix(timestampInt, 0)
	return os.Chtimes(filePath, newTime, newTime)
}
