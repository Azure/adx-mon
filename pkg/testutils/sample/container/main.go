package main

import (
	"log/slog"
	"math/rand"
	"os"
	"time"
)

func main() {
	jhandler := slog.NewJSONHandler(os.Stdout, nil)
	jlog := slog.New(jhandler)
	jlog = jlog.WithGroup("handler").With(slog.String("encoding", "json"))

	jlog.Info("Begin")
	for {
		<-time.After(time.Second)

		randomNumber := rand.Intn(3) + 1 // Generate a random number between 1 and 3
		switch randomNumber {
		case 1:
			jlog.Info("All is well", "number", randomNumber)
		case 2:
			jlog.Warn("Something is wrong", "number", randomNumber)
		case 3:
			jlog.Error("Something is very wrong", "number", randomNumber)
		}
	}
}
