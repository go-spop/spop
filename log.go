package spoe

type Logger interface {
	Errorf(format string, args ...any)
	Warnf(format string, args ...any)
	Infof(format string, args ...any)
	Debugf(format string, args ...any)
	Tracef(format string, args ...any)
}

type nillogger struct{}

func (nillogger) Errorf(_ string, _ ...any) {}
func (nillogger) Warnf(_ string, _ ...any)  {}
func (nillogger) Infof(_ string, _ ...any)  {}
func (nillogger) Debugf(_ string, _ ...any) {}
func (nillogger) Tracef(_ string, _ ...any) {}
