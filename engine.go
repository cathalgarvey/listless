package main

import (
	"errors"
	"io"
	"log"
	"net/smtp"
	"strings"
	"time"

	"github.com/jordan-wright/email"
	"github.com/layeh/gopher-luar"
	"github.com/tgulacsi/imapclient"
	"github.com/yuin/gopher-lua"
)

var (
	// ErrErrValNotStringOrNil - returned from ProcessMail when the 'error' value in eventLoop is not a string or nil.
	ErrErrValNotStringOrNil = errors.New("'error' value returned from eventLoop function in Lua is neither string nor nil type")
	// ErrOkNotBoolean - returned from ProcessMail when the 'ok' value in eventLoop is absent or not boolean.
	ErrOkNotBoolean = errors.New("'ok' value returned from eventLoop function in Lua is not boolean")
)

// Engine is the state and event looper that manages the account and list.
type Engine struct {
	Lua      *lua.LState
	DB       *ListlessDB
	Client   imapclient.Client
	Config   *Config
	Shutdown chan struct{}
}

// NewEngine - Return a new Engine from the given config.
func NewEngine(cfg *Config) (*Engine, error) {
	var err error
	if cfg == nil {
		log.Fatal("Cannot start a Listless Engine with an empty configuration.")
	}
	E := new(Engine)
	E.Config = cfg
	E.Lua = lua.NewState()
	E.DB, err = NewDatabase(cfg.Database)
	//E.DB, err = bolt.Open(cfg.Database, 0600, nil)
	if err != nil {
		return nil, err
	}
	E.Client = imapclient.NewClientTLS(cfg.IMAPHost, cfg.IMAPPort, cfg.IMAPUsername, cfg.IMAPPassword)
	E.Shutdown = make(chan struct{})
	return E, nil
}

// Close all open database, scripting engine and IMAP connections.
func (eng *Engine) Close() {
	log.Println("Shutting down..")
	close(eng.Shutdown)
	eng.Lua.Close()
	eng.DB.Close()
	eng.Client.Close(true)
}

// ProcessMail takes an email struct, passes is to the Lua script, and applies
// any edits *in place* on the email.
func (eng *Engine) ProcessMail(e *Email) (ok bool, err error) {
	log.Println("Received email: " + e.Subject)
	log.Println("Normalising recipient lists..")
	e.NormaliseRecipients()
	log.Println("Loading user eventLoop script..")
	// Execute user-defined script in Lua Runtime, in a child thread of the base
	// engine.
	// This function doesn't appear to add any references to the child thread to
	// the parent, nor to push the child thread onto the parent's stack, so I think
	// when this thread goes out of scope it will be garbage collected without
	// extra effort.
	L := eng.Lua.NewThread()
	err = L.DoFile(eng.Config.DeliverScript)
	if err != nil {
		return false, err
	}
	log.Println("Calling `eventLoop` function from Lua")
	// Run expected "eventLoop" function with arguments "database", "message".
	err = L.CallByParam(
		lua.P{
			Fn:      L.GetGlobal("eventLoop"),
			NRet:    3, // Number of returned arguments?
			Protect: true,
		},
		luar.New(L, eng.Config),
		luar.New(L, eng.DB),
		luar.New(L, e))
	if err != nil {
		return false, err
	}
	// Get three returned arguments, do something about them.
	//e2 := eng.Lua.Get(1)     // message to send; should be same as e, verify?
	errmsg := L.Get(3) // Either a string error or nil
	if !(errmsg.Type() == lua.LTString || errmsg.Type() == lua.LTNil) {
		return false, ErrErrValNotStringOrNil
	}
	okv := L.Get(2) // Boolean
	if !(okv.Type() == lua.LTBool) {
		return false, ErrOkNotBoolean
	}
	if !(okv.String() == "true") {
		// All OK, just don't send any messages today.
		return false, nil
	}
	return true, nil
}

// Handler is the main loop that handles incoming mail - It satisfies the DeliverFunc
// interface required by imapclient but is a method attached to a set of rich state
// objects.
func (eng *Engine) Handler(r io.ReadSeeker, uid uint32, sha1 []byte) error {
	imapclient.LongSleep = time.Duration(eng.Config.PollFrequency) * time.Second
	thismail, err := email.NewEmailFromReader(r)
	if err != nil {
		log.Println("Received email but failed to parse: " + err.Error())
		return err
	}
	// Check for header indicating this was sent BY the list to itself (common pattern)
	if thismail.Headers.Get("sent-from-listless") == eng.Config.ListAddress {
		return nil
	}
	log.Println("Received mail addressed TO: " + strings.Join(thismail.To, ", "))
	luaMail := WrapEmail(thismail)
	ok, err := eng.ProcessMail(luaMail)
	if err != nil {
		log.Println("Error calling ProcessMail handler: " + err.Error())
		return err
	}
	if !ok {
		log.Println("No error occurred but not sending message.")
		return nil
	}
	log.Println("Outgoing email subject: " + luaMail.Subject)
	// Set header to indicate that this was sent by Listless, in case it loops around
	// somehow (some lists retain the "To: <list@address.com>" header unchanged).
	luaMail.Headers.Set("sent-from-listless", eng.Config.ListAddress)
	auth := smtp.PlainAuth("", eng.Config.SMTPUsername, eng.Config.SMTPPassword, eng.Config.SMTPHost)
	//auth := smtp.PlainAuth(eng.Config.SMTPUsername, eng.Config.SMTPUsername, eng.Config.SMTPPassword, eng.Config.SMTPHost)
	// Patched to allow excluding of variadic emails added after auth.
	err = luaMail.Send(eng.Config.smtpAddr, auth, eng.Config.ListAddress)
	if err != nil {
		log.Println("Error sending message by SMTP: " + err.Error())
		return err
	}
	log.Println("Sent message successfully: " + luaMail.Subject)
	return nil
}

// ExecOnce - This is exec Mode: Load config and database, ignore eventLoop script.
// Inject the database into the runtime, and execute the given string as exec Script.
// Can later add helper functions for Exec mode, like a CSV parser to mass-add
// list subscribers.
func (eng *Engine) ExecOnce(script string) error {
	L := eng.Lua.NewThread()
	L.SetGlobal("database", luar.New(L, eng.DB))
	return L.DoString(script)
}
