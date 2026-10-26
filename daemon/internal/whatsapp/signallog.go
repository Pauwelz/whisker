package whatsapp

import "go.mau.fi/libsignal/logger"

// silentLog drops every log line of libsignal. Its error lines hold raw bytes of remote Messages.
type silentLog struct{}

func (silentLog) Debug(caller, message string)   {}
func (silentLog) Info(caller, message string)    {}
func (silentLog) Warning(caller, message string) {}
func (silentLog) Error(caller, message string)   {}
func (silentLog) Configure(settings string)      {}

// libsignal falls back to a logger that prints to stdout. Replace it before any library code runs.
func init() {
	var l logger.Loggable = silentLog{}
	logger.Setup(&l)
}
