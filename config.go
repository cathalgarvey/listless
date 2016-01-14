package main

import (
	"log"
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
	ListAddress   string
	Database      string
	DeliverScript string
	Constants     map[string]string
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
// * Constants    map/table of string->string values. This can be used to store
//     data which is made available in each iteration of eventLoop.
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
	C.ListAddress = stringOrNothing(L.GetGlobal("ListAddress"))
	C.Database = stringOrNothing(L.GetGlobal("Database"))
	C.DeliverScript = stringOrNothing(L.GetGlobal("DeliverScript"))
	// Sane defaults
	if C.IMAPPort == -1 {
		log.Println("Assuming port 143 for IMAPPort config option.")
		C.IMAPPort = 143
	}
	if C.SMTPPort == -1 {
		log.Println("Assuming port 465 for SMTPPort config option.")
		C.SMTPPort = 465
	}
	C.smtpAddr = C.SMTPHost + ":" + strconv.Itoa(C.SMTPPort)
	if C.ListAddress == "" {
		C.ListAddress = C.SMTPUsername + "@" + C.SMTPHost
		log.Println("Setting 'ListAddress' configuration option to " + C.ListAddress + " as this field is required and must be reasonably unique. Set manually if incorrect.")
	}
	C.Constants = make(map[string]string)
	if constantsTable, ok := L.GetGlobal("Constants").(*lua.LTable); ok {
		constantsTable.ForEach(func(key, val lua.LValue) {
			C.Constants[key.String()] = val.String()
		})
	}
	log.Println("SMTP Address: " + C.smtpAddr)
	return C
}
