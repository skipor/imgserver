package imgserver

import "github.com/Sirupsen/logrus"

const (
	//logger field to indicate log msg emitter
	FromLoggerFieldKey = "from"
)

type Logger interface {
	logrus.FieldLogger
}

func SetEmitter(log Logger, emitter string) Logger {
	return log.WithField(FromLoggerFieldKey, emitter)
}
