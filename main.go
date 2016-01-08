package main

import (
	"io"
	"log"
	"net/smtp"
	"strconv"

	"github.com/alecthomas/kingpin"
	"github.com/boltdb/bolt"
	"github.com/jordan-wright/email"
	"github.com/layeh/gopher-luar"
	"github.com/tgulacsi/imapclient"
	"github.com/yuin/gopher-lua"
)

var (
	configfile = kingpin.Arg("configfile", "Location of config file.").Required().String()
)

func main() {
	configL := lua.NewState()
	config := ConfigFromState(configL)
	engine, err := New(config)
	if err != nil {
		log.Fatal(err)
	}
	// Setup main loop, run forevs.
	imapclient.DeliveryLoop(engine.Client, "INBOX", "", engine.Handler, "", "", engine.Shutdown)
	log.Println("Exited DeliveryLoop successfully, shutting down.")
}

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

// Converts a Lua state to a Config object; expects the following variables to
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
	C.IMAPPassword = stringOrNothing(L.GetGlobal("IMAPUsername"))
	C.IMAPHost = stringOrNothing(L.GetGlobal("IMAPUsername"))
	C.IMAPPort = intOrNothing(L.GetGlobal("IMAPUsername"))
	C.SMTPUsername = stringOrNothing(L.GetGlobal("SMTPUsername"))
	C.SMTPPassword = stringOrNothing(L.GetGlobal("SMTPUsername"))
	C.SMTPHost = stringOrNothing(L.GetGlobal("SMTPUsername"))
	C.SMTPPort = intOrNothing(L.GetGlobal("SMTPUsername"))
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

// Engine is the state and event looper that manages the account and list.
type Engine struct {
	Lua      *lua.LState
	DB       *bolt.DB
	Client   imapclient.Client
	Config   *Config
	Shutdown chan struct{}
}

// New - Return a new Engine from the given config.
func New(cfg *Config) (*Engine, error) {
	var err error
	if cfg == nil {
		log.Fatal("Cannot start a Listless Engine with an empty configuration.")
	}
	E := new(Engine)
	E.Config = cfg
	E.Lua = lua.NewState()
	E.DB, err = bolt.Open(cfg.Database, 0600, nil)
	if err != nil {
		return nil, err
	}
	E.Client = imapclient.NewClientTLS(cfg.IMAPHost, cfg.IMAPPort, cfg.IMAPUsername, cfg.IMAPPassword)
	E.Shutdown = make(chan struct{})
	return E, nil
}

func (eng *Engine) Close() {
	defer eng.Lua.Close()
	defer eng.DB.Close()
	defer eng.Client.Close(true)
	eng.Shutdown <- struct{}{}
}

// ProcessMail takes an email struct, passes is to the Lua script, and applies
// any edits *in place* on the email.
func (eng *Engine) ProcessMail(e *email.Email) error {
	// Get lua script each time; mail volume is not expected to be so high that this
	// will become a serious cost, and it allows live edits to the processing code.
	eng.Lua.SetGlobal("email", luar.New(eng.Lua, e))
	return eng.Lua.DoFile(eng.Config.DeliverScript)
}

// Handler is the main loop that handles incoming mail - It satisfies the DeliverFunc
// interface required by imapclient but is a method attached to a set of rich state
// objects.
func (eng *Engine) Handler(r io.ReadSeeker, uid uint32, sha1 []byte) error {
	thismail, err := email.NewEmailFromReader(r)
	if err != nil {
		return err
	}
	err = eng.ProcessMail(thismail)
	if err != nil {
		return err
	}
	auth := smtp.PlainAuth("", eng.Config.SMTPUsername, eng.Config.SMTPPassword, eng.Config.SMTPHost)
	thismail.Send(eng.Config.smtpAddr, auth)
	return nil
}
