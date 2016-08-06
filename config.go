package main

import (
	"net"
	"strconv"

	"gopkg.in/inconshreveable/log15.v2"

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
	SMTPIP       string
	// Local stuff
	ListAddress      string
	Database         string
	DeliverScript    string
	MessageFrequency int
	PollFrequency    int // Seconds
	Constants        map[string]string
}

// Returns "" if failed to parse.
func stringOrNothing(l lua.LValue) string {
	if l.Type() != lua.LTString {
		return ""
	}
	return l.String()
}

// Returns -1 if failed.
func intOrDefault(l lua.LValue, def int) int {
	if l.Type() != lua.LTNumber {
		return -1
	}
	i, err := strconv.Atoi(l.String())
	if err != nil {
		return def
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
	C.IMAPPort = intOrDefault(L.GetGlobal("IMAPPort"), 143)
	C.SMTPUsername = stringOrNothing(L.GetGlobal("SMTPUsername"))
	C.SMTPPassword = stringOrNothing(L.GetGlobal("SMTPPassword"))
	C.SMTPHost = stringOrNothing(L.GetGlobal("SMTPHost"))
	C.SMTPPort = intOrDefault(L.GetGlobal("SMTPPort"), 465)
	C.ListAddress = stringOrNothing(L.GetGlobal("ListAddress"))
	C.Database = stringOrNothing(L.GetGlobal("Database"))
	C.DeliverScript = stringOrNothing(L.GetGlobal("DeliverScript"))
	C.MessageFrequency = intOrDefault(L.GetGlobal("MessageFrequency"), 1)
	C.PollFrequency = intOrDefault(L.GetGlobal("PollFrequency"), 60)
	C.smtpAddr = C.SMTPHost + ":" + strconv.Itoa(C.SMTPPort)
	C.SMTPIP = stringOrNothing(L.GetGlobal("SMTPIP"))
	if C.SMTPIP == "" {
		// Guess IP address by seeking DNS host for SMTPHost
		ips, err := net.LookupIP(C.SMTPHost)
		if err != nil {
			panic(err)
		}
		if len(ips) != 1 {
			panic("Failed to get unambiguous IP for SMTP server, to validate SPF records")
		}
		log15.Info("Using lookup-derived IP for SMTPHost as SMTPIP (for SPF)", log15.Ctx{"context": "setup", "SMTPIP": ips[0].String(), "SMTPHost": C.SMTPHost})
		C.SMTPIP = ips[0].String()
	}
	if C.ListAddress == "" {
		C.ListAddress = C.SMTPUsername + "@" + C.SMTPHost
		log15.Info("Creating a uniquey 'ListAddress' config option as none was provided manually", log15.Ctx{"context": "setup", "ListAddress": C.ListAddress})
	}
	C.Constants = make(map[string]string)
	if constantsTable, ok := L.GetGlobal("Constants").(*lua.LTable); ok {
		constantsTable.ForEach(func(key, val lua.LValue) {
			C.Constants[key.String()] = val.String()
		})
	}
	log15.Info("SMTP Address..", log15.Ctx{"context": "setup", "SMTP Address": C.smtpAddr})
	return C
}
