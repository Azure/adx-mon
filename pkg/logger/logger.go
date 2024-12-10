package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/go-logr/logr"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

const timeFormat = "2006-01-02T15:04:05.000000Z07:00"

var (
	levelVar = new(slog.LevelVar)
	debugmsg = slog.StringValue("DBG")
	infomsg  = slog.StringValue("INF")
	warnmsg  = slog.StringValue("WRN")
	errormsg = slog.StringValue("ERR")
)

func init() {
	var logger *slog.Logger

	if !isatty.IsTerminal(os.Stdout.Fd()) {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levelVar, ReplaceAttr: replaceAttr}))
	} else {
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{Level: levelVar, TimeFormat: timeFormat}))
	}
	slog.SetDefault(logger)

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		return
	}

	switch level {
	case "ERROR":
		levelVar.Set(slog.LevelError)
	case "WARN":
		levelVar.Set(slog.LevelWarn)
	case "INFO":
		levelVar.Set(slog.LevelInfo)
	case "DEBUG":
		levelVar.Set(slog.LevelDebug)
	case "TRACE":
		levelVar.Set(slog.LevelDebug)
	default:
		fmt.Printf("Unknown log level: %s != [ERROR,WARN,INFO,DEBUG,TRACE]\n", level)
	}
}

func LogrHandler() logr.Logger {
	return logr.FromSlogHandler(slog.Default().Handler())
}

func SetLevel(level slog.Level) {
	levelVar.Set(level)
}

func IsDebug() bool {
	return slog.Default().Enabled(context.Background(), slog.LevelDebug)
}

func IsInfo() bool {
	return slog.Default().Enabled(context.Background(), slog.LevelInfo)
}

func IsWarn() bool {
	return slog.Default().Enabled(context.Background(), slog.LevelWarn)
}

var (
	Debug = slog.Debug
	Info  = slog.Info
	Warn  = slog.Warn
	Error = slog.Error
	// Fatal defined below
)

// For these wrappers, we need to capture the caller's PC to allow source line functionality to work.
// This does not add any allocs and does not affect performance.

func Debugf(format string, args ...interface{}) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Infof]
	r := slog.NewRecord(time.Now(), slog.LevelDebug, fmt.Sprintf(format, args...), pcs[0])
	_ = logger.Handler().Handle(context.Background(), r)
}

func Infof(format string, args ...interface{}) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Infof]
	r := slog.NewRecord(time.Now(), slog.LevelInfo, fmt.Sprintf(format, args...), pcs[0])
	_ = logger.Handler().Handle(context.Background(), r)
}

func Warnf(format string, args ...interface{}) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Infof]
	r := slog.NewRecord(time.Now(), slog.LevelWarn, fmt.Sprintf(format, args...), pcs[0])
	_ = logger.Handler().Handle(context.Background(), r)
}

func Errorf(format string, args ...interface{}) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelError) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Infof]
	r := slog.NewRecord(time.Now(), slog.LevelError, fmt.Sprintf(format, args...), pcs[0])
	_ = logger.Handler().Handle(context.Background(), r)
}

func Fatal(msg string, attrs ...slog.Attr) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelError) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Infof]
	r := slog.NewRecord(time.Now(), slog.LevelError, msg, pcs[0])
	r.AddAttrs(attrs...)
	_ = logger.Handler().Handle(context.Background(), r)
	os.Exit(1)
}

func Fatalf(format string, args ...interface{}) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelError) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Infof]
	r := slog.NewRecord(time.Now(), slog.LevelError, fmt.Sprintf(format, args...), pcs[0])
	_ = logger.Handler().Handle(context.Background(), r)
	os.Exit(1)
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Key = "ts"
		a.Value = slog.AnyValue(a.Value.Any().(time.Time).UTC().Format(timeFormat))
	}
	if a.Key == slog.LevelKey {
		a.Key = "lvl"

		level := a.Value.Any().(slog.Level)
		switch {
		case level < slog.LevelInfo:
			a.Value = debugmsg
		case level < slog.LevelWarn:
			a.Value = infomsg
		case level < slog.LevelError:
			a.Value = warnmsg
		default:
			a.Value = errormsg
		}
	}

	return a
}
