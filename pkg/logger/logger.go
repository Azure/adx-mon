package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

const timeFormat = "2006-01-02T15:04:05.000000Z07:00"

type LogLevel int

const (
	LevelError LogLevel = iota
	LevelWarn
	LevelInfo
	LevelDebug
	LevelTrace
)

var (
	Default = NewLogger()
	red     = color.New(color.FgRed).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	yellow  = color.New(color.FgYellow).SprintFunc()
	blue    = color.New(color.FgHiBlue).SprintFunc()
	cyan    = color.New(color.FgCyan).SprintFunc()

	levelDesc = map[int]string{
		1: "ERR",
		2: "WRN",
		3: "INF",
		4: "DBG",
		5: "TRC",
	}

	descLevel = map[string]int{
		"ERR": 1,
		"WRN": 2,
		"INF": 3,
		"DBG": 4,
		"TRC": 5,
	}
)

func init() {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		Default.SetFormatter(&JsonFormatter{})
	}

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		return
	}

	switch level {
	case "ERROR":
		Default.SetLevel(LevelError)
	case "WARN":
		Default.SetLevel(LevelWarn)
	case "INFO":
		Default.SetLevel(LevelInfo)
	case "DEBUG":
		Default.SetLevel(LevelDebug)
	case "TRACE":
		Default.SetLevel(LevelTrace)
	default:
		fmt.Printf("Unknown log level: %s != [ERROR,WARN,INFO,DEBUG,TRACE]\n", level)
	}
}

type Formatter interface {
	Format(ts, level, format string, args ...interface{}) string
}

type TextFormatter struct{}

func (t *TextFormatter) Format(ts, level, format string, args ...interface{}) string {
	return fmt.Sprintf(fmt.Sprintf("%s %s %s", ts, level, format), args...)
}

type JsonFormatter struct{}

func (t *JsonFormatter) Format(ts, level, format string, args ...interface{}) string {
	msg, _ := json.Marshal(map[string]string{
		"ts":  ts,
		"lvl": level,
		"msg": fmt.Sprintf(format, args...),
	})
	return string(msg)
}

// Logger is the interface for logging.
type Logger interface {
	Trace(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})

	SetLevel(level LogLevel)
	IsTrace() bool
	IsDebug() bool
	IsInfo() bool
	IsWarn() bool
	SetFormatter(formatter Formatter)
}

func NewLogger() Logger {
	l := &logger{
		formatter: &TextFormatter{},
	}
	l.SetLevel(LevelInfo)
	return l

}

type logger struct {
	logLevel  LogLevel
	formatter Formatter
}

func (l *logger) SetFormatter(formatter Formatter) {
	l.formatter = formatter
}

func (l *logger) Trace(format string, args ...interface{}) {
	if !l.IsTrace() {
		return
	}

	l.level(cyan("TRC"), format, args...)
}

func (l *logger) Debug(format string, args ...interface{}) {
	if !l.IsDebug() {
		return
	}

	l.level(blue("DBG"), format, args...)
}

func (l *logger) Info(format string, args ...interface{}) {
	if !l.IsInfo() {
		return
	}

	l.level(green("INF"), format, args...)
}

func (l *logger) Warn(format string, args ...interface{}) {
	if !l.IsWarn() {
		return
	}

	l.level(yellow("WRN"), format, args...)
}

func (l *logger) Error(format string, args ...interface{}) {
	if !l.IsError() {
		return
	}

	l.level(red("ERR"), format, args...)
}

func (l *logger) level(level, format string, args ...interface{}) {
	ts := time.Now().UTC().Format(timeFormat)
	msg := l.formatter.Format(ts, level, format, args...)
	fmt.Println(msg)
}

func (l *logger) SetLevel(lvl LogLevel) { l.logLevel = lvl }
func (l *logger) IsTrace() bool         { return l.logLevel >= LevelTrace }
func (l *logger) IsDebug() bool         { return l.logLevel >= LevelDebug }
func (l *logger) IsInfo() bool          { return l.logLevel >= LevelInfo }
func (l *logger) IsWarn() bool          { return l.logLevel >= LevelWarn }
func (l *logger) IsError() bool         { return l.logLevel >= LevelError }

func IsTrace() bool {
	return Default.IsTrace()
}

func Tracef(format string, args ...interface{}) {
	Default.Trace(format, args...)
}

func IsDebug() bool {
	return Default.IsDebug()
}

func Debugf(format string, args ...interface{}) {
	Default.Debug(format, args...)
}

func Infof(format string, args ...interface{}) {
	Default.Info(format, args...)
}

func Warnf(format string, args ...interface{}) {
	Default.Warn(format, args...)
}

func Errorf(format string, args ...interface{}) {
	Default.Error(format, args...)
}

func Fatalf(format string, args ...interface{}) {
	Default.Error(format, args...)
	os.Exit(1)
}
