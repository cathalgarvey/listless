package main

import (
	"strconv"

	"github.com/yuin/gopher-lua"
)

// Config options required for listless to work.
type Config struct {
	// IMAP Details
	IMAPUsername string
	IMAPPassword string
	IMAPHost     string
	IMAPPort     int
	// SMTP Details
	SMTPUsername string
	SMTPPassword string
	SMTPHost     string
	SMTPPort     int
	smtpAddr     string
	// Local stuff
	Database      string
	DeliverScript string
}

// Returns "" if failed to parse.
func stringOrNothing(l lua.LValue) string {
	if l.Type() != lua.LTString {
		return ""
	}
	return l.String()
}

// Returns -1 if failed.
func intOrNothing(l lua.LValue) int {
	if l.Type() != lua.LTNumber {
		return -1
	}
	i, err := strconv.Atoi(l.String())
	if err != nil {
		return -1
	}
	return i
}

// ConfigFromState converts a Lua state to a Config object; expects the following variables to
// be defined, or defaults to either accepted default port numbers or empty strings:
// * IMAPUsername string
// * IMAPPassword string
// * IMAPHost     string
// * IMAPPort     int
// * SMTPUsername string
// * SMTPPassword string
// * SMTPHost     string
// * SMTPPort     int
// * Database      string
// * DeliverScript string
func ConfigFromState(L *lua.LState) *Config {
	C := new(Config)
	C.IMAPUsername = stringOrNothing(L.GetGlobal("IMAPUsername"))
	C.IMAPPassword = stringOrNothing(L.GetGlobal("IMAPPassword"))
	C.IMAPHost = stringOrNothing(L.GetGlobal("IMAPHost"))
	C.IMAPPort = intOrNothing(L.GetGlobal("IMAPPort"))
	C.SMTPUsername = stringOrNothing(L.GetGlobal("SMTPUsername"))
	C.SMTPPassword = stringOrNothing(L.GetGlobal("SMTPPassword"))
	C.SMTPHost = stringOrNothing(L.GetGlobal("SMTPHost"))
	C.SMTPPort = intOrNothing(L.GetGlobal("SMTPPort"))
	C.Database = stringOrNothing(L.GetGlobal("Database"))
	C.DeliverScript = stringOrNothing(L.GetGlobal("DeliverScript"))
	// Sane defaults
	if C.IMAPPort == -1 {
		C.IMAPPort = 143
	}
	if C.SMTPPort == -1 {
		C.SMTPPort = 465
	}
	C.smtpAddr = C.SMTPHost + ":" + strconv.Itoa(C.SMTPPort)
	return C
}
