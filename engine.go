package main

import (
	"errors"
	"io"
	"log"
	"net/smtp"
	"strings"

	"github.com/dchest/validator"
	"github.com/jordan-wright/email"
	"github.com/layeh/gopher-luar"
	"github.com/tgulacsi/imapclient"
	"github.com/yuin/gopher-lua"
)

// Engine is the state and event looper that manages the account and list.
type Engine struct {
	Lua      *lua.LState
	DB       *ListlessDB
	Client   imapclient.Client
	Config   *Config
	Shutdown chan struct{}
}

// Email is a derivation of email.Email with a few methods added to play nicely
// in lua.
type Email struct {
	*email.Email
	// Set-like map to keep track of who's already in a recipient list, whether
	// "To", "CC", or "BCC".
	inRecipientLists map[string]struct{}
}

// WrapEmail - given an email.Email object, return the wrapper used in this
// package to provide Lua scripting conveniences.
func WrapEmail(e *email.Email) *Email {
	newe := new(Email)
	newe.inRecipientLists = make(map[string]struct{})
	return newe
}

// Check recipient roster
func (em *Email) isRecipient(email string) bool {
	_, present := em.inRecipientLists[email]
	return present
}

// Add to recipient roster
func (em *Email) addRecipient(email string) {
	em.inRecipientLists[email] = struct{}{}
}

// Remove from recipient roster
func (em *Email) remRecipient(email string) {
	delete(em.inRecipientLists, email)
}

// AddToRecipient directly adds someone to the To list.
// Emails are normalised before addition or removal.
func (em *Email) AddToRecipient(email string) {
	email = validator.NormalizeEmail(email)
	if !em.isRecipient(email) {
		em.To = append(em.To, email)
		em.addRecipient(email)
	}
}

// AddCcRecipient directly adds someone to the CC list.
// Emails are normalised before addition or removal.
func (em *Email) AddCcRecipient(email string) {
	email = validator.NormalizeEmail(email)
	if !em.isRecipient(email) {
		em.Cc = append(em.Cc, email)
		em.addRecipient(email)
	}
}

// AddBccRecipient directly adds someone to the BCC list.
// Emails are normalised before addition or removal.
func (em *Email) AddBccRecipient(email string) {
	email = validator.NormalizeEmail(email)
	if !em.isRecipient(email) {
		em.Bcc = append(em.Bcc, email)
		em.addRecipient(email)
	}
}

// AddRecipient is a shortcut for AddBccRecipient.
func (em *Email) AddRecipient(email string) {
	em.AddBccRecipient(email)
}

// AddRecipientList adds a slice/list-table of recipients to the BCC list. This
// is the usual/recommended way to add subscribers to the list.
func (em *Email) AddRecipientList(emails []string) {
	for _, e := range emails {
		em.AddRecipient(e)
	}
}

// RemoveRecipient looks for and removes a recipient email. If not found, no
// error is raised. This is an expensive operation; reallocates To/CC/BCC!
func (em *Email) RemoveRecipient(email string) {
	email = validator.NormalizeEmail(email)
	// Efficiency!
	if _, present := em.inRecipientLists[email]; !present {
		return
	}
	newTo := make([]string, 0, len(em.To))
	for _, e := range em.To {
		if e == email {
			continue
		}
		newTo = append(newTo, e)
	}
	em.To = newTo
	newCc := make([]string, 0, len(em.Cc))
	for _, e := range em.Cc {
		if e == email {
			continue
		}
		newCc = append(newCc, e)
	}
	em.Cc = newCc
	newBcc := make([]string, 0, len(em.Bcc))
	for _, e := range em.Bcc {
		if e == email {
			continue
		}
		newBcc = append(newBcc, e)
	}
	em.Bcc = newBcc
	em.remRecipient(email)
}

// NormaliseRecipients runs through the To, CC, and BCC lists and normalises all
// emails contained therein, as well as deduplicating emails in that order.
// This is run prior to calling eventLoop in Lua, and added emails are all
// normalised under the hood, so there should be no need to call this from Lua.
func (em *Email) NormaliseRecipients() {
	newTo := make([]string, 0, len(em.To))
	for _, e := range em.To {
		e = validator.NormalizeEmail(e)
		if _, ok := em.inRecipientLists[e]; ok {
			continue
		} else {
			em.inRecipientLists[e] = struct{}{}
		}
		newTo = append(newTo, e)
	}
	em.To = newTo

	newCc := make([]string, 0, len(em.Cc))
	for _, e := range em.Cc {
		e = validator.NormalizeEmail(e)
		if _, ok := em.inRecipientLists[e]; ok {
			continue
		} else {
			em.inRecipientLists[e] = struct{}{}
		}
		newCc = append(newCc, e)
	}
	em.Cc = newCc

	newBcc := make([]string, 0, len(em.Bcc))
	for _, e := range em.Bcc {
		e = validator.NormalizeEmail(e)
		if _, ok := em.inRecipientLists[e]; ok {
			continue
		} else {
			em.inRecipientLists[e] = struct{}{}
		}
		newBcc = append(newBcc, e)
	}
	em.Bcc = newBcc
}

// GetHeader is a direct call to email.Headers.Get
func (em *Email) GetHeader(key string) string {
	return em.Headers.Get(key)
}

// AddHeader is a direct call to email.Headers.Add
func (em *Email) AddHeader(key, value string) {
	em.Headers.Add(key, value)
}

// DelHeader is a direct call to email.Headers.Del
func (em *Email) DelHeader(key string) {
	em.Headers.Del(key)
}

// SetHeader is a direct call to email.Headers.Set
func (em *Email) SetHeader(key, value string) {
	em.Headers.Set(key, value)
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
	// Execute user-defined script in Lua Runtime..
	err = eng.Lua.DoFile(eng.Config.DeliverScript)
	if err != nil {
		return false, err
	}
	log.Println("Calling `eventLoop` function from Lua")
	// Run expected "eventLoop" function with arguments "database", "message".
	err = eng.Lua.CallByParam(
		lua.P{
			Fn:      eng.Lua.GetGlobal("eventLoop"),
			NRet:    3, // Number of returned arguments?
			Protect: true,
		},
		luar.New(eng.Lua, eng.DB),
		luar.New(eng.Lua, e))
	if err != nil {
		return false, err
	}
	// Get three returned arguments, do something about them.
	//e2 := eng.Lua.Get(1)     // message to send; should be same as e, verify?
	errmsg := eng.Lua.Get(3) // Either a string error or nil
	if !(errmsg.Type() == lua.LTString || errmsg.Type() == lua.LTNil) {
		return false, ErrErrValNotStringOrNil
	}
	okv := eng.Lua.Get(2) // Boolean
	if !(okv.Type() == lua.LTBool) {
		return false, ErrOkNotBoolean
	}
	if !(okv.String() == "true") {
		// All OK, just don't send any messages today.
		return false, nil
	}
	return true, nil
}

var (
	// ErrErrValNotStringOrNil - returned from ProcessMail when the 'error' value in eventLoop is not a string or nil.
	ErrErrValNotStringOrNil = errors.New("'error' value returned from eventLoop function in Lua is neither string nor nil type")
	// ErrOkNotBoolean - returned from ProcessMail when the 'ok' value in eventLoop is absent or not boolean.
	ErrOkNotBoolean = errors.New("'ok' value returned from eventLoop function in Lua is not boolean")
)

// Handler is the main loop that handles incoming mail - It satisfies the DeliverFunc
// interface required by imapclient but is a method attached to a set of rich state
// objects.
func (eng *Engine) Handler(r io.ReadSeeker, uid uint32, sha1 []byte) error {
	thismail, err := email.NewEmailFromReader(r)
	if err != nil {
		log.Println("Received email but failed to parse: " + err.Error())
		return err
	}
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
	log.Println("Sending email to member list: " + strings.Join(luaMail.To, ", "))
	auth := smtp.PlainAuth("", eng.Config.SMTPUsername, eng.Config.SMTPPassword, eng.Config.SMTPHost)
	err = luaMail.Send(eng.Config.smtpAddr, auth)
	if err != nil {
		log.Println("Error sending message by SMTP: " + err.Error())
		return err
	}
	return errors.New("Temporary error so my personal mail isn't all marked as read")
	//	return nil
}
