/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package fixer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/bradfitz/latlong"
)

// Struct to hold the structure of the JSON metadata
type imageMetadata struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"`
		Formatted string `json:"formatted"`
	} `json:"photoTakenTime"`
	GeoData struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Altitude  float64 `json:"altitude"`
	} `json:"geoData"`
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

	return data, json.Unmarshal(byteValue, &data)
}

// Helper to find exiftool (bundled or in PATH)
func getExifToolPath() string {
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		exifName := "exiftool"
		if runtime.GOOS == "windows" {
			exifName = "exiftool.exe"
		}
		bundledPath := filepath.Join(dir, exifName)
		if _, err := os.Stat(bundledPath); err == nil {
			return bundledPath
		}
	}
	return "exiftool"
}

// Start a persistent exiftool process (internal version, assumes lock is held)
func initializeExifToolInternal() error {
	if exifToolCmd != nil {
		return nil
	}

	exifToolCmd = exec.Command(getExifToolPath(), "-stay_open", "True", "-@", "-")

	var err error
	exifToolStdin, err = exifToolCmd.StdinPipe()
	if err != nil {
		return err
	}

	exifToolStdout, err = exifToolCmd.StdoutPipe()
	if err != nil {
		return err
	}
	exifToolScanner = bufio.NewScanner(exifToolStdout)

	if err := exifToolCmd.Start(); err != nil {
		return err
	}

	return nil
}

// Start a persistent exiftool process
func InitializeExifTool() error {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()
	return initializeExifToolInternal()
}

// Close the persistent exiftool process
func CloseExifTool() {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	if exifToolCmd != nil {
		exifToolStdin.Write([]byte("-stay_open\nFalse\n"))
		exifToolCmd.Wait()
		exifToolCmd = nil
	}
}

// runExifTool executes a command on the persistent exiftool process with a timeout and restart logic.
func runExifTool(args []string) ([]string, error) {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	// Ensure process is running
	if exifToolCmd == nil {
		if err := initializeExifToolInternal(); err != nil {
			return nil, fmt.Errorf("failed to restart exiftool: %v", err)
		}
	}

	command := strings.Join(args, "\n") + "\n-execute\n"
	if _, err := exifToolStdin.Write([]byte(command)); err != nil {
		exifToolCmd = nil
		if err := initializeExifToolInternal(); err != nil {
			return nil, fmt.Errorf("exiftool restart failed after write error: %v", err)
		}
		if _, err := exifToolStdin.Write([]byte(command)); err != nil {
			return nil, fmt.Errorf("exiftool second write attempt failed: %v", err)
		}
	}

	var lines []string
	var exifErr error
	done := make(chan bool, 1)
	
	// Track the current process to ensure the goroutine is tied to it
	currentScanner := exifToolScanner

	go func() {
		for currentScanner.Scan() {
			line := currentScanner.Text()
			if line == "{ready}" {
				done <- true
				return
			}
			if strings.Contains(line, "Error") && exifErr == nil {
				exifErr = fmt.Errorf("exiftool error: %s", line)
			}
			lines = append(lines, line)
		}
		done <- false
	}()

	// Heartbeat logic: only kicks in for slow files (> 5s)
	startTime := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var targetFile string
	lastSize := int64(-1)

	for {
		select {
		case success := <-done:
			if !success {
				exifToolCmd = nil
				return nil, fmt.Errorf("exiftool process closed unexpectedly")
			}
			return lines, exifErr
		case <-ticker.C:
			elapsed := time.Since(startTime).Round(time.Second)
			
			// Only on the first slow-down, identify what we are working on
			if targetFile == "" {
				for _, arg := range args {
					if !strings.HasPrefix(arg, "-") && (strings.HasSuffix(strings.ToLower(arg), ".jpg") || strings.HasSuffix(strings.ToLower(arg), ".png") || strings.HasSuffix(strings.ToLower(arg), ".heic") || strings.HasSuffix(strings.ToLower(arg), ".mp4") || strings.HasSuffix(strings.ToLower(arg), ".mov")) {
						targetFile = arg
						break
					}
				}
			}

			// Empirical check: Is the temp file growing?
			if targetFile != "" {
				tempFile := targetFile + "_exiftool_tmp"
				if info, err := os.Stat(tempFile); err == nil {
					currentSize := info.Size()
					if currentSize > lastSize {
						Log(LoggerInfo, "Slow file detected: %s (Temp file growing: %s)", filepath.Base(targetFile), formatSize(currentSize))
						lastSize = currentSize
						continue 
					}
				}
			}
			
			Log(LoggerInfo, "Still working... (exiftool active for %s)", elapsed)
		case <-time.After(60 * time.Second):
			if exifToolCmd != nil && exifToolCmd.Process != nil {
				_ = exifToolCmd.Process.Kill()
			}
			exifToolCmd = nil
			return nil, fmt.Errorf("exiftool timed out after 60s of no activity")
		}
	}
}

// Helper to format file sizes
func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Apply all available metadata to a file
func ApplyMetadata(filePath string, meta imageMetadata) error {
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	utcTime := time.Unix(timestampInt, 0).UTC()

	// Determine timezone at the photo's GPS location.
	photoLoc := getPhotoTimezone(meta.GeoData.Latitude, meta.GeoData.Longitude)
	localTime := utcTime.In(photoLoc)
	_, offsetSec := localTime.Zone()
	offsetStr := formatTimezoneOffset(offsetSec)

	exifTime := localTime.Format("2006:01:02 15:04:05")
	exifTimeWithTZ := exifTime + offsetStr

	// Common arguments for both images and videos
	args := []string{"-overwrite_original"}
	if meta.Title != "" {
		args = append(args, "-Title="+meta.Title)
	}
	if meta.Description != "" {
		args = append(args, "-ImageDescription="+meta.Description, "-Caption-Abstract="+meta.Description)
	}
	if meta.GeoData.Latitude != 0 && meta.GeoData.Longitude != 0 {
		lat, lon := meta.GeoData.Latitude, meta.GeoData.Longitude
		latRef, lonRef := "N", "E"
		if lat < 0 {
			latRef = "S"
		}
		if lon < 0 {
			lonRef = "W"
		}
		args = append(args,
			fmt.Sprintf("-GPSLatitude=%f", math.Abs(lat)),
			fmt.Sprintf("-GPSLatitudeRef=%s", latRef),
			fmt.Sprintf("-GPSLongitude=%f", math.Abs(lon)),
			fmt.Sprintf("-GPSLongitudeRef=%s", lonRef),
			fmt.Sprintf("-GPSAltitude=%f", meta.GeoData.Altitude),
		)
	}

	if IsVideoFile(filePath) {
		args = append(args,
			"-CreateDate="+exifTimeWithTZ,
			"-ModifyDate="+exifTimeWithTZ,
			"-TrackCreateDate="+exifTimeWithTZ,
			"-MediaCreateDate="+exifTimeWithTZ,
			"-FileCreateDate="+exifTimeWithTZ,
			"-FileModifyDate="+exifTimeWithTZ,
			"-OffsetTimeOriginal="+offsetStr,
		)
	} else {
		args = append(args,
			"-AllDates="+exifTimeWithTZ,
			"-FileCreateDate="+exifTimeWithTZ,
			"-FileModifyDate="+exifTimeWithTZ,
			"-OffsetTime="+offsetStr,
			"-OffsetTimeOriginal="+offsetStr,
			"-OffsetTimeDigitized="+offsetStr,
		)
	}
	args = append(args, filePath)

	_, err = runExifTool(args)
	if err != nil {
		Log(LoggerWarn, "Exiftool can't write metadata to %s: %v", filepath.Base(filePath), err)
	}

	// Set file birth time (creation date) using macOS SetFile
	if err := SetFileBirthTime(filePath, localTime); err != nil {
		Log(LoggerWarn, "Failed to set birth time for %s: %v", filePath, err)
	}

	return nil
}

<<<<<<< HEAD
// ReadExifIdentity reads DateTimeOriginal, ImageWidth, ImageHeight for dedup comparison
func ReadExifIdentity(filePath string) (dateOriginal string, width string, height string, err error) {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	if exifToolCmd == nil {
		return "", "", "", fmt.Errorf("exiftool not initialized")
	}

	if _, err := fmt.Fprintf(exifToolStdin, "-DateTimeOriginal\n-ImageWidth\n-ImageHeight\n-s3\n-charset\nfilename=utf8\n%s\n-execute\n", filePath); err != nil {
		return "", "", "", err
	}

	var lines []string
	for exifToolScanner.Scan() {
		line := exifToolScanner.Text()
		if line == "{ready}" {
			break
		}
		if !strings.Contains(line, "Error") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}

	if scanErr := exifToolScanner.Err(); scanErr != nil {
		return "", "", "", scanErr
	}

	if len(lines) >= 3 {
		return lines[0], lines[1], lines[2], nil
	}
	return "", "", "", fmt.Errorf("incomplete EXIF identity for %s", filepath.Base(filePath))
}

// GetMajorBrand reads the MajorBrand tag from a file using the persistent exiftool instance
func GetMajorBrand(filePath string) (string, error) {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()
=======
var (
	setFileAvailable     bool
	setFileAvailableOnce sync.Once
)
>>>>>>> main

func checkSetFileAvailable() bool {
	setFileAvailableOnce.Do(func() {
		if runtime.GOOS != "darwin" {
			return
		}
		_, err := exec.LookPath("SetFile")
		setFileAvailable = err == nil
		if !setFileAvailable {
			Log(LoggerWarn, "SetFile not found — install Xcode Command Line Tools (xcode-select --install) to set file creation dates")
		}
	})
	return setFileAvailable
}

func SetFileBirthTime(filePath string, t time.Time) error {
	if !checkSetFileAvailable() {
		return nil
	}
	setfileFormat := t.Format("01/02/2006 15:04:05")
	cmd := exec.Command("SetFile", "-d", setfileFormat, filePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SetFile failed: %v, output: %s", err, string(output))
	}
	return nil
}

// ReadExifDate reads DateTimeOriginal from a file's existing EXIF data
func ReadExifDate(filePath string) (time.Time, error) {
	args := []string{"-DateTimeOriginal", "-CreateDate", "-s3", "-charset", "filename=utf8", filePath}
	lines, err := runExifTool(args)
	if err != nil {
		return time.Time{}, err
	}

	var dateStr string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			dateStr = strings.TrimSpace(line)
			break
		}
	}

	if dateStr == "" {
		return time.Time{}, fmt.Errorf("no EXIF date found")
	}

	for _, layout := range []string{
		"2006:01:02 15:04:05",
		"2006:01:02 15:04:05-07:00",
		"2006:01:02 15:04:05+07:00",
	} {
		if t, err := time.Parse(layout, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse EXIF date: %s", dateStr)
}

// ReadExifIdentity reads DateTimeOriginal, ImageWidth, ImageHeight for dedup comparison
func ReadExifIdentity(filePath string) (dateOriginal string, width string, height string, err error) {
	args := []string{"-DateTimeOriginal", "-ImageWidth", "-ImageHeight", "-s3", "-charset", "filename=utf8", filePath}
	lines, err := runExifTool(args)
	if err != nil {
		return "", "", "", err
	}

	if len(lines) >= 3 {
		return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), strings.TrimSpace(lines[2]), nil
	}
	return "", "", "", fmt.Errorf("incomplete EXIF identity for %s", filepath.Base(filePath))
}

// GetMajorBrand reads the MajorBrand tag from a file using the persistent exiftool instance
func GetMajorBrand(filePath string) (string, error) {
	args := []string{"-MajorBrand", "-s3", "-charset", "filename=utf8", filePath}
	lines, err := runExifTool(args)
	if err != nil {
		return "", err
	}

	if len(lines) > 0 {
		return strings.TrimSpace(lines[0]), nil
	}
	return "", fmt.Errorf("no MajorBrand found")
}


// Determine the timezone at a photo's GPS location using the "latlog" library
// If no GPS data is available, fall back to local time
func getPhotoTimezone(lat, lon float64) *time.Location {
	if lat == 0 && lon == 0 {
		return time.Local
	}

	tzName := latlong.LookupZoneName(lat, lon)
	if tzName == "" {
		// Fallback in case latlog fails to find a timezone
		Log(LoggerWarn, "Could not look up timezone for coordinates lat=%f, lon=%f", lat, lon)
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Fallback in case loading timezone fails
		Log(LoggerWarn, "Could not load timezone '%s'", tzName)
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}
	return loc
}

// Format a timezone offset in seconds as "+hh:mm" / "-hh:mm" for EXIF
// for example 3600 seconds becomes "+01:00"
func formatTimezoneOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	minutes := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

// Exiftool process variables
var (
	exifToolCmd     *exec.Cmd
	exifToolStdin   io.WriteCloser
	exifToolStdout  io.ReadCloser
	exifToolScanner *bufio.Scanner
	exifToolMutex   sync.Mutex
)
