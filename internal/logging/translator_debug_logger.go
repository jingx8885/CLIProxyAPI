// Package logging provides translator debug logging functionality.
// This file implements a dedicated debug logger for translator input/output
// that writes to a separate file to avoid cluttering the main log.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	translatorDebugMu     sync.Mutex
	translatorDebugWriter *lumberjack.Logger
	translatorDebugFile   *os.File
	translatorDebugEnabled bool
)

// EnableTranslatorDebugLog enables the translator debug log and creates the log file.
// The log file is created in the logs directory with the name "translator_debug.log".
func EnableTranslatorDebugLog() error {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()

	if translatorDebugEnabled {
		return nil
	}

	logDir := "logs"
	if base := util.WritablePath(); base != "" {
		logDir = filepath.Join(base, "logs")
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("translator debug log: failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, "translator_debug.log")
	translatorDebugWriter = &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    50, // 50MB per file
		MaxBackups: 3,  // Keep 3 old files
		MaxAge:     1,  // 1 day
		Compress:   false,
	}

	translatorDebugEnabled = true
	fmt.Printf("[translator_debug] Debug log enabled, writing to: %s\n", logPath)
	return nil
}

// DisableTranslatorDebugLog disables the translator debug log and closes the log file.
func DisableTranslatorDebugLog() {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()

	if !translatorDebugEnabled {
		return
	}

	if translatorDebugWriter != nil {
		_ = translatorDebugWriter.Close()
		translatorDebugWriter = nil
	}
	if translatorDebugFile != nil {
		_ = translatorDebugFile.Close()
		translatorDebugFile = nil
	}

	translatorDebugEnabled = false
}

// IsTranslatorDebugLogEnabled returns whether the translator debug log is enabled.
func IsTranslatorDebugLogEnabled() bool {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()
	return translatorDebugEnabled
}

// LogTranslatorInput logs the raw input to the translator.
func LogTranslatorInput(translatorName, requestID string, rawJSON []byte) {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()

	if !translatorDebugEnabled || translatorDebugWriter == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	header := fmt.Sprintf("\n[%s] [%s] [%s] === INPUT ===\n", timestamp, requestID, translatorName)
	_, _ = translatorDebugWriter.Write([]byte(header))
	_, _ = translatorDebugWriter.Write(rawJSON)
	_, _ = translatorDebugWriter.Write([]byte("\n"))
}

// LogTranslatorOutput logs the output from the translator.
func LogTranslatorOutput(translatorName, requestID string, output []string) {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()

	if !translatorDebugEnabled || translatorDebugWriter == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	header := fmt.Sprintf("[%s] [%s] [%s] === OUTPUT ===\n", timestamp, requestID, translatorName)
	_, _ = translatorDebugWriter.Write([]byte(header))
	for i, o := range output {
		_, _ = translatorDebugWriter.Write([]byte(fmt.Sprintf("[%d]: %s\n", i, o)))
	}
	_, _ = translatorDebugWriter.Write([]byte("=== END ===\n"))
}

// LogTranslatorEvent logs a specific event in the translator.
func LogTranslatorEvent(translatorName, requestID, event string, data map[string]interface{}) {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()

	if !translatorDebugEnabled || translatorDebugWriter == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	header := fmt.Sprintf("[%s] [%s] [%s] EVENT: %s\n", timestamp, requestID, translatorName, event)
	_, _ = translatorDebugWriter.Write([]byte(header))
	for k, v := range data {
		_, _ = translatorDebugWriter.Write([]byte(fmt.Sprintf("  %s: %v\n", k, v)))
	}
}

// LogTranslatorWarning logs a warning in the translator.
func LogTranslatorWarning(translatorName, requestID, message string) {
	translatorDebugMu.Lock()
	defer translatorDebugMu.Unlock()

	if !translatorDebugEnabled || translatorDebugWriter == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] [%s] WARNING: %s\n", timestamp, requestID, translatorName, message)
	_, _ = translatorDebugWriter.Write([]byte(line))
}
