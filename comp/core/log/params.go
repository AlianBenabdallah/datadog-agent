// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package log

import (
	"runtime"

	"github.com/DataDog/datadog-agent/pkg/config"
)

// Params defines the parameters for this log component.
//
// Logs-related parameters are implemented as unexported fields containing
// callbacks.  These fields can be set with the `LogXxx()` methods, which
// return the updated LogParams.  One of `LogForOneShot` or `LogForDaemon`
// must be called.
type Params struct {
	// LoggerName is the name that appears in the logfile
	LoggerName string

	// LogLevelFn returns the log level. This field is set by methods on this
	// type.
	LogLevelFn func(configGetter) string

	// LogFileFn returns the log file. This field is set by methods on this type.
	LogFileFn func(configGetter) string

	// LogSyslogURIFn returns the syslog URI. This field is set by methods on this type.
	LogSyslogURIFn func(configGetter) string

	// LogSyslogRFCFn returns a boolean determining whether to use syslog RFC
	// 5424. This field is set by methods on this type.
	LogSyslogRFCFn func(configGetter) bool

	// LogToConsoleFn returns a boolean determining whether to write logs to
	// the console. This field is set by methods on this type.
	LogToConsoleFn func(configGetter) bool

	// LogFormatJSONFn returns a boolean determining whether logs should be
	// written in JSON format.
	LogFormatJSONFn func(configGetter) bool
}

// configGetter is a subset of the comp/core/config component, able to get
// config values for the xxxFn fields in LogParams.  comp/core/log uses
// this interface to get parameters that may depend on a configuration value.
type configGetter interface {
	GetString(key string) string
	GetBool(key string) bool
}

// LogForOneShot sets up logging parameters for a one-shot app.
//
// If overrideFromEnv is set, then DD_LOG_LEVEL will override the given level.
//
// Otherwise, file logging is disabled, syslog is disabled, console logging is
// enabled, and JSON formatting is disabled.
func LogForOneShot(loggerName, level string, overrideFromEnv bool) Params {
	params := Params{}
	params.LoggerName = loggerName
	if overrideFromEnv {
		params.LogLevelFn = func(configGetter) string { return config.GetEnvDefault("DD_LOG_LEVEL", level) }
	} else {
		params.LogLevelFn = func(configGetter) string { return level }
	}
	params.LogFileFn = func(configGetter) string { return "" }
	params.LogSyslogURIFn = func(configGetter) string { return "" }
	params.LogSyslogRFCFn = func(configGetter) bool { return false }
	params.LogToConsoleFn = func(configGetter) bool { return true }
	params.LogFormatJSONFn = func(configGetter) bool { return false }
	return params
}

// LogForDaemon sets up logging parameters for a daemon app.
//
// The log level is set based on the `log_level` config parameter.
//
// The log file is set based on the logFileConfig config parameter,
// or disabled if `disable_file_logging` is set.
//
// On platforms which support it, syslog is enabled if `log_to_syslog` is set,
// using `syslog_uri` or defaulting to "unixgram:///dev/log" if that is empty.
// The `syslog_rfc` config parameter determines whether this produces 5424-compliant
// output.
//
// Console logging is enabled if `log_to_console` is set.  Lots are formatted
// as JSON if `log_format_json` is set.
func LogForDaemon(loggerName, logFileConfig, defaultLogFile string) Params {
	params := Params{}
	params.LoggerName = loggerName
	params.LogLevelFn = func(g configGetter) string { return g.GetString("log_level") }
	params.LogFileFn = func(g configGetter) string {
		if g.GetBool("disable_file_logging") {
			return ""
		}
		logFile := g.GetString(logFileConfig)
		if logFile == "" {
			logFile = defaultLogFile
		}
		return logFile
	}
	params.LogSyslogURIFn = func(g configGetter) string {
		if runtime.GOOS == "windows" {
			return "" // syslog not supported on Windows
		}
		enabled := g.GetBool("log_to_syslog")
		uri := g.GetString("syslog_uri")

		if !enabled {
			return ""
		}

		if uri == "" {
			return "unixgram:///dev/log"
		}

		return uri
	}
	params.LogSyslogRFCFn = func(g configGetter) bool { return g.GetBool("syslog_rfc") }
	params.LogToConsoleFn = func(g configGetter) bool { return g.GetBool("log_to_console") }
	params.LogFormatJSONFn = func(g configGetter) bool { return g.GetBool("log_format_json") }
	return params
}

// LogToFile modifies the parameters to set the destination log file, overriding any
// previous logfile parameter.
func (params *Params) LogToFile(logFile string) {
	params.LogFileFn = func(configGetter) string { return logFile }
}