package tail

import (
	"fmt"

	"github.com/Azure/adx-mon/pkg/logger"
)

type tailLogger struct{}

func (t *tailLogger) Fatal(v ...interface{}) {
	logger.Fatal(fmt.Sprint(v...))
}

func (t *tailLogger) Fatalf(format string, v ...interface{}) {
	logger.Fatalf(format, v...)
}

func (t *tailLogger) Fatalln(v ...interface{}) {
	logger.Fatal(fmt.Sprintln(v...))
}

func (t *tailLogger) Panic(v ...interface{}) {
	msg := fmt.Sprint(v...)
	logger.Error(msg)
	panic(msg)
}

func (t *tailLogger) Panicf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Error(msg)
	panic(msg)
}

func (t *tailLogger) Panicln(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	logger.Error(msg)
	panic(msg)
}

func (t *tailLogger) Print(v ...interface{}) {
	logger.Info(fmt.Sprint(v...))
}

func (t *tailLogger) Printf(format string, v ...interface{}) {
	logger.Infof(format, v...)
}

func (t *tailLogger) Println(v ...interface{}) {
	logger.Info(fmt.Sprintln(v...))
}
