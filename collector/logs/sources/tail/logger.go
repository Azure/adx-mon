package tail

import (
	"fmt"

	"github.com/Azure/adx-mon/pkg/logger"
)

type tailLogger struct{}

func (t *tailLogger) Fatal(v ...any) {
	logger.Fatal(fmt.Sprint(v...))
}

func (t *tailLogger) Fatalf(format string, v ...any) {
	logger.Fatalf(format, v...)
}

func (t *tailLogger) Fatalln(v ...any) {
	logger.Fatal(fmt.Sprintln(v...))
}

func (t *tailLogger) Panic(v ...any) {
	msg := fmt.Sprint(v...)
	logger.Error(msg)
	panic(msg)
}

func (t *tailLogger) Panicf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	logger.Error(msg)
	panic(msg)
}

func (t *tailLogger) Panicln(v ...any) {
	msg := fmt.Sprintln(v...)
	logger.Error(msg)
	panic(msg)
}

func (t *tailLogger) Print(v ...any) {
	logger.Info(fmt.Sprint(v...))
}

func (t *tailLogger) Printf(format string, v ...any) {
	logger.Infof(format, v...)
}

func (t *tailLogger) Println(v ...any) {
	logger.Info(fmt.Sprintln(v...))
}
