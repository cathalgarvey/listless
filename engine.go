package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/smtp"
	"strings"
	"time"

	"github.com/cjoudrey/gluaurl"
	"github.com/jordan-wright/email"
	luajson "github.com/layeh/gopher-json"
	// "github.com/cjoudrey/gluahttp"
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
		return nil, errors.New("Fatal error, Cannot load Listless engine with empty configuration.")
	}
	E := new(Engine)
	E.Config = cfg
	E.Lua = lua.NewState()
	// Preload a few extra libs..
	luajson.Preload(E.Lua)
	E.Lua.PreloadModule("url", gluaurl.Loader)
	// Disabled for security, right now:
	// E.Lua.PreloadModule("http", gluahttp.NewHttpModule(&http.Client{}).Loader)
	E.DB, err = NewDatabase(cfg.Database)
	if err != nil {
		return nil, err
	}
	E.Client = imapclient.NewClientTLS(cfg.IMAPHost, cfg.IMAPPort, cfg.IMAPUsername, cfg.IMAPPassword)
	E.Shutdown = make(chan struct{})
	err = applyLuarWhitelists(E.Lua)
	if err != nil {
		luaLog.Error("Error setting method whitelists in lua runtime: %s", err.Error())
		return nil, err
	}
	return E, nil
}

// Close all open database, scripting engine and IMAP connections.
func (eng *Engine) Close() {
	llLog.Info("Shutting down..")
	close(eng.Shutdown)
	eng.Lua.Close()
	eng.DB.Close()
	eng.Client.Close(true)
}

// ModeratorSandbox creates a new lua state for executing mod commands. The state
// is fresh and should be deleted afterwards.
// ModeratorSandbox can execute an arbitrary lua script in a more tightly constrained
// execution environment intended to enable subscriber add/remove ops, or bans, or
// queued messages, etc.
// Exposes database but with a reduced subset of methods.
// Exposes a copy of config; changes are not saved.
func (eng *Engine) ModeratorSandbox() (*lua.LState, error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	for _, opener := range []lua.LGFunction{
		lua.OpenPackage,
		lua.OpenBase,
		lua.OpenString,
		lua.OpenTable,
		lua.OpenMath,
		lua.OpenCoroutine,
		lua.OpenChannel,
	} {
		opener(L)
	}
	err := applyLuarWhitelists(L)
	if err != nil {
		luaLog.Error("Error setting method whitelists in lua runtime: %s", err.Error())
		return nil, err
	}
	// Set globals for Moderator. Config is a copy. Database is wrapped in ModeratorDBWrapper.
	L.SetGlobal("database", luar.New(L, eng.DB.ModeratorDBWrapper()))
	// Need an authentic copy of the config file guaranteed to have no mutable refs.
	// Screw manual reflective deep-copying, let's just JSON-cycle this sh*t
	confJSON, err := json.Marshal(eng.Config)
	if err != nil {
		return nil, err
	}
	tmpConf := new(Config)
	err = json.Unmarshal(confJSON, tmpConf)
	if err != nil {
		return nil, err
	}
	// Globalise
	L.SetGlobal("config", luar.New(L, tmpConf))
	return L, nil
}

// PrivilegedSandbox returns the default sandbox used for executing eventLoop.
// This sandbox is not much of a box and is not remotely safe to run untrusted
// code within.
func (eng *Engine) PrivilegedSandbox() *lua.LState {
	L := eng.Lua.NewThread()
	L.OpenLibs() // ALL THE LIBS
	return L
}

// ProcessMail takes an email struct, passes is to the Lua script, and applies
// any edits *in place* on the email.
func (eng *Engine) ProcessMail(e *Email) (ok bool, err error) {
	imapLog.Info("Received email: " + e.Subject)
	imapLog.Info("Normalising recipient lists..")
	e.NormaliseRecipients()
	luaLog.Info("Loading user eventLoop script..")
	// Execute user-defined script in Lua Runtime, in a child thread of the base
	// engine.
	// This function doesn't appear to add any references to the child thread to
	// the parent, nor to push the child thread onto the parent's stack, so I think
	// when this thread goes out of scope it will be garbage collected without
	// extra effort.
	L := eng.PrivilegedSandbox()
	err = L.DoFile(eng.Config.DeliverScript)
	if err != nil {
		luaLog.Error("Error loading eventLoop file: %s", err.Error())
		return false, err
	}
	luaLog.Info("Calling `eventLoop` function from Lua")
	// Database object with whitelisted methods; the whitelist is in NewEngine
	privDB := luar.New(L, eng.DB.PrivilegedDBWrapper())
	evTimer := luaLog.Timer()
	// Run expected "eventLoop" function with arguments "database", "message".
	err = L.CallByParam(
		lua.P{
			Fn:      L.GetGlobal("eventLoop"),
			NRet:    3, // Number of returned arguments?
			Protect: true,
		},
		luar.New(L, eng.Config),
		privDB,
		luar.New(L, e))
	if err != nil {
		luaLog.Error("Error executing eventLoop function: %s", err.Error())
		return false, err
	}
	evTimer.End("Executed eventLoop successfully.")
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
	thismail, err := email.NewEmailFromReader(r)
	if err != nil {
		imapLog.Error("Received email but failed to parse: %s", err.Error())
		return err
	}
	// Check for header indicating this was sent BY the list to itself (common pattern)
	if thismail.Headers.Get("sent-from-listless") == eng.Config.ListAddress {
		imapLog.Info("Received mail with a sent-from-listless header matching own. Ignoring.")
		return nil
	}
	imapLog.Info("Received mail addressed TO: %s", strings.Join(thismail.To, ", "))
	luaMail := WrapEmail(thismail)
	ok, err := eng.ProcessMail(luaMail)
	if err != nil {
		luaLog.Error("Error calling ProcessMail handler: %s", err.Error())
		return err
	}
	if !ok {
		smtpLog.Info("No error occurred but not sending message on instruction from Lua.")
		return nil
	}
	smtpLog.Info("Outgoing email subject: %s", luaMail.Subject)
	// Set header to indicate that this was sent by Listless, in case it loops around
	// somehow (some lists retain the "To: <list@address.com>" header unchanged).
	luaMail.Headers.Set("sent-from-listless", eng.Config.ListAddress)
	auth := smtp.PlainAuth("", eng.Config.SMTPUsername, eng.Config.SMTPPassword, eng.Config.SMTPHost)
	//auth := smtp.PlainAuth(eng.Config.SMTPUsername, eng.Config.SMTPUsername, eng.Config.SMTPPassword, eng.Config.SMTPHost)
	// Patched to allow excluding of variadic emails added after auth.
	err = luaMail.Send(eng.Config.smtpAddr, auth, eng.Config.ListAddress)
	if err != nil {
		smtpLog.Error("Error sending message by SMTP: %s", err.Error())
		return err
	}
	smtpLog.Info("Sent message successfully: %s", luaMail.Subject)
	return nil
}

// DeliveryLoop is the poll loop for listless, mostly lifted from imapclient.
func (eng *Engine) DeliveryLoop(c imapclient.Client, inbox, pattern string, deliver imapclient.DeliverFunc, outbox, errbox string, closeCh <-chan struct{}) {
	if inbox == "" {
		inbox = "INBOX"
	}
	for {
		n, err := imapclient.DeliverOne(c, inbox, pattern, deliver, outbox, errbox)
		if err != nil {
			imapLog.Error("Error during DeliveryLoop cycle - Deliveries %d; Error: %s", n, err.Error())
		} else {
			imapLog.Info("DeliveryLoop delivered: ", n)
		}
		select {
		case _, ok := <-closeCh:
			if !ok { //channel is closed
				return
			}
		default:
		}

		if err != nil {
			<-time.After(time.Duration(eng.Config.PollFrequency) * time.Second)
			continue
		}
		if n > 0 {
			<-time.After(time.Duration(eng.Config.MessageFrequency) * time.Second)
		} else {
			<-time.After(time.Duration(eng.Config.PollFrequency) * time.Second)
		}
		continue
	}
}

// ExecOnce - This is exec Mode: Load config and database, ignore eventLoop script.
// Inject the database into the runtime, and execute the given string as exec Script.
// Can later add helper functions for Exec mode, like a CSV parser to mass-add
// list subscribers.
func (eng *Engine) ExecOnce(script string) error {
	L := eng.Lua.NewThread()
	L.SetGlobal("config", luar.New(L, eng.Config))
	L.SetGlobal("database", luar.New(L, eng.DB))
	return L.DoString(script)
}
