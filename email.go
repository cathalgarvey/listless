package main

import (
	"errors"
	"log"
	"net/mail"
	"net/smtp"
	"strings"

	"github.com/dchest/validator"
	"github.com/jordan-wright/email"
	"github.com/layeh/gopher-luar"
	"github.com/yuin/gopher-lua"
)

// ErrEmailUnparseable - Returned when a To/CC/BCC entry can't be parsed into a simple Email address.
var ErrEmailUnparseable = errors.New("Email appears neither a simple ('foo@bar.com') nor expressive ('Foo Bar <foo@bar.com>') construction")

// Email is a derivation of email.Email with a few methods added to play nicely
// in lua.
type Email struct {
	*email.Email
	// Set-like map to keep track of who's already in a recipient list, whether
	// "To", "CC", or "BCC".
	inRecipientLists map[string]struct{}
	Sender           string
}

// WrapEmail - given an email.Email object, return the wrapper used in this
// package to provide Lua scripting conveniences.
func WrapEmail(e *email.Email) *Email {
	newe := new(Email)
	newe.Email = e
	newe.inRecipientLists = make(map[string]struct{})
	sender, _ := parseExpressiveEmail(e.From)
	newe.Sender = normaliseEmail(sender)
	return newe
}

// GetText returns the message Text as a string. Warning: Encoding-naive!
// This returns the text body, not a HTML body if included in the mail!
func (em *Email) GetText() string {
	return string(em.Text)
}

// SetText sets the email Text as a given string. This replaces the existing
// Body/Text.
// This sets the text body, not HTML!
func (em *Email) SetText(newtext string) {
	em.Text = append(em.Text[:0], []byte(newtext)...)
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

// Clear all recipients in roster
func (em *Email) clearRecipients() {
	em.inRecipientLists = make(map[string]struct{})
}

// AddToRecipient directly adds someone to the To list.
// Emails are normalised before addition or removal.
func (em *Email) AddToRecipient(email string) {
	email = normaliseEmail(email)
	if email == "" {
		return
	}
	if !em.isRecipient(email) {
		em.To = append(em.To, email)
		em.addRecipient(email)
	}
}

// AddCcRecipient directly adds someone to the CC list.
// Emails are normalised before addition or removal.
func (em *Email) AddCcRecipient(email string) {
	email = normaliseEmail(email)
	if email == "" {
		return
	}
	if !em.isRecipient(email) {
		em.Cc = append(em.Cc, email)
		em.addRecipient(email)
	}
}

// AddBccRecipient directly adds someone to the BCC list.
// Emails are normalised before addition or removal.
func (em *Email) AddBccRecipient(email string) {
	email = normaliseEmail(email)
	if email == "" {
		return
	}
	if !em.isRecipient(email) {
		em.Bcc = append(em.Bcc, email)
		em.addRecipient(email)
	}
}

// AddRecipient is a shortcut for AddBccRecipient.
func (em *Email) AddRecipient(email string) {
	em.AddBccRecipient(email)
}

// AddRecipientList adds a lua list-table of recipients to the BCC list. This
// is the usual/recommended way to add subscribers to the list.
func (em *Email) AddRecipientList(L *luar.LState) int {
	if recipientListTable, ok := L.Get(1).(*lua.LTable); ok {
		recipientListTable.ForEach(func(idx, emailAddrV lua.LValue) {
			em.AddRecipient(emailAddrV.String())
		})
		return 0
	}
	L.RaiseError("AddRecipientList expected a table, got something else.")
	return 0
}

func (em *Email) goAddRecipientList(emails []string) {
	for _, e := range emails {
		em.AddRecipient(e)
	}
}

// ClearRecipients removes all To/CC/BCC recipients.
func (em *Email) ClearRecipients() {
	em.To = em.To[:0]
	em.Cc = em.Cc[:0]
	em.Bcc = em.Bcc[:0]
	em.clearRecipients()
}

// RemoveRecipient looks for and removes a recipient email. If not found, no
// error is raised. This is an expensive operation; reallocates To/CC/BCC!
// To minimise impact this assumes the roster of emails is correct and that
// email normalisation successfully deduplicated recipients, so it stops after
// the first such reallocation that encounters the specified email address.
func (em *Email) RemoveRecipient(email string) {
	email = normaliseEmail(email)
	// Efficiency!
	if _, present := em.inRecipientLists[email]; !present {
		return
	}

	removed := false

	newTo := make([]string, 0, len(em.To))
	for _, e := range em.To {
		if e == email {
			removed = true
			continue
		}
		newTo = append(newTo, e)
	}
	em.To = append(em.To[:0], newTo...)

	// Minor efficiencies; assuming normalisation already deduplicated all these
	// lists, and that the recipient set is accurate, then having removed the
	// address from any one list it should be assumed absent already from the rest.
	if !removed {
		newCc := make([]string, 0, len(em.Cc))
		for _, e := range em.Cc {
			if e == email {
				removed = true
				continue
			}
			newCc = append(newCc, e)
		}
		em.Cc = append(em.Cc[:0], newCc...)
	}

	if !removed {
		newBcc := make([]string, 0, len(em.Bcc))
		for _, e := range em.Bcc {
			if e == email {
				continue
			}
			newBcc = append(newBcc, e)
		}
		em.Bcc = append(em.Bcc[:0], newBcc...)
	}

	// Remove from recipient set
	em.remRecipient(email)
}

// This patches over issues with the `email` package where sometimes the "To"
// header is a single string as the first entry in the "To" field of the Email
// struct. It also DRYs out the NormaliseRecipients function. To help the Logger,
// this function accepts a string arg naming the field under iteration.
// This adds all seen emails to the Email.inRecipientLists set.
func (em *Email) normaliseEmailSlice(field string, emailSlice []string) []string {
	if len(emailSlice) == 0 {
		return nil
	}
	newField := make([]string, 0, len(emailSlice))
	for _, entry := range emailSlice {
		// First, split multi-entry bits if necessary.. Look for ">" chars that don't
		// end the line, and try to extract emails from each such substring using
		// parseExpressiveEmail()
		// TODO: Replace parseMultiExpressiveEmails with https://golang.org/pkg/net/mail/#ParseAddressList
		multiEntries := parseMultiExpressiveEmails(entry)
		for _, e := range multiEntries {
			e, err := parseExpressiveEmail(e)
			if err != nil {
				log.Println("Error parsing address from '" + field + "' email recipient: " + e)
				continue
			}
			if _, ok := em.inRecipientLists[e]; ok {
				log.Println("Skipping recipient as it's already been seen: " + e)
				continue
			} else {
				em.inRecipientLists[e] = struct{}{}
			}
			newField = append(newField, e)
		}
	}
	return newField
}

// NormaliseRecipients runs through the To, CC, and BCC lists and normalises all
// emails contained therein, as well as deduplicating emails in that order.
// This is run prior to calling eventLoop in Lua, and added emails are all
// normalised under the hood, so there should be no need to call this from Lua.
func (em *Email) NormaliseRecipients() {
	newTo := em.normaliseEmailSlice("To", em.To)
	if newTo != nil {
		em.To = append(em.To[:0], newTo...)
	}
	newCc := em.normaliseEmailSlice("Cc", em.Cc)
	if newCc != nil {
		em.Cc = append(em.Cc[:0], newCc...)
	}
	newBcc := em.normaliseEmailSlice("Bcc", em.Bcc)
	if newBcc != nil {
		em.Bcc = append(em.Bcc[:0], newBcc...)
	}
}

// Strangely, validator doesn't ToLower emails, so "normalisation" can be defeated
// by different casing. As I'm using it to dedupe and keep track of emails, this
// isn't good enough..
func normaliseEmail(email string) string {
	email = strings.ToLower(email)
	return validator.NormalizeEmail(email)
}

// parseExpressiveEmail - Given a line "Foo Bar <foo@bar.com>", return "foo@bar.com".
// For "foo@bar.com" return simply that!
func parseExpressiveEmail(emailLine string) (string, error) {
	emailLine = strings.TrimSpace(emailLine)
	if normE := normaliseEmail(emailLine); normE != "" {
		return normE, nil
	}
	// - Must have the brackets
	if !(strings.Contains(emailLine, "<") && strings.Contains(emailLine, ">")) {
		return emailLine, ErrEmailUnparseable
	}
	// Brackets must come in correct order.
	openBr := strings.LastIndex(emailLine, "<")
	closBr := strings.LastIndex(emailLine, ">")
	if !(openBr < closBr) {
		return emailLine, ErrEmailUnparseable
	}
	parsed := emailLine[openBr+1 : closBr]
	normed := normaliseEmail(parsed)
	if normed == "" {
		return emailLine, ErrEmailUnparseable
	}
	return normed, nil
}

// Given a string like "Cathal Garvey <cathal@foo.com>, Stephen Barr <steve@foo.com>"
// return []string{"Cathal Garvey <cathal@foo.com>", "Stephen Barr <steve@foo.com>"}
func parseMultiExpressiveEmails(entry string) []string {
	// Shortcuts
	if validator.IsValidEmail(entry) {
		return []string{entry}
	}
	if !(strings.Contains(entry, ">") && strings.Contains(entry, ",")) {
		return []string{entry}
	}
	var entries []string
	// Find each comma-after-closebracket and slice around it.
	for i := 0; i < len(entry); {
		nextBracket := strings.Index(entry[i:], ">")
		if nextBracket == -1 {
			entries = append(entries, entry[i:])
			break
		}
		// nextComma gets the index *after the bracket* so needs to be added to
		// the nextBracket index!
		distCommaAfterBracket := strings.Index(entry[nextBracket:], ",")
		if distCommaAfterBracket == -1 {
			entries = append(entries, entry[i:])
			break
		}
		nextComma := distCommaAfterBracket + nextBracket
		chunk := entry[i:nextComma]
		entries = append(entries, chunk)
		i = nextComma + 1
	}
	return entries
}

// Send an email using the given host and SMTP auth (optional), returns any error thrown by smtp.SendMail
// This function merges the To, Cc, and Bcc fields and calls the smtp.SendMail function using the Email.Bytes() output as the message
// Shadows the Send method of email.Email because:
//  - The email roster already provides a list of recipients, so it'll be a little
//    more efficient
//  - (More urgently) avoid bounce notices by avoiding sending to the list address!
func (em *Email) Send(addr string, a smtp.Auth, excludeEmails ...string) error {
	nuexcludeEmails := make(map[string]struct{})
	for _, e := range excludeEmails {
		e = normaliseEmail(e)
		if e == "" {
			continue
		}
		nuexcludeEmails[e] = struct{}{}
	}
	// Merge the To, Cc, and Bcc fields, minus excluded emails.
	to := make([]string, 0, len(em.To)+len(em.Cc)+len(em.Bcc)-len(nuexcludeEmails))
	for k := range em.inRecipientLists {
		if _, ok := nuexcludeEmails[k]; ok {
			continue
		}
		to = append(to, k)
	}
	for i := 0; i < len(to); i++ {
		addr, err := mail.ParseAddress(to[i])
		if err != nil {
			return err
		}
		to[i] = addr.Address
	}
	// Check to make sure there is at least one recipient and one "From" address
	if em.From == "" || len(to) == 0 {
		return errors.New("Must specify at least one From address and one To address")
	}
	from, err := mail.ParseAddress(em.From)
	if err != nil {
		return err
	}
	raw, err := em.Bytes()
	if err != nil {
		return err
	}
	return smtp.SendMail(addr, a, from.Address, to, raw)
}
