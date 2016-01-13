package main

import (
	"errors"
	"log"
	"strings"

	"github.com/dchest/validator"
	"github.com/jordan-wright/email"
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
	newe.Sender = strings.ToLower(normaliseEmail(sender))
	return newe
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

// AddToRecipient directly adds someone to the To list.
// Emails are normalised before addition or removal.
func (em *Email) AddToRecipient(email string) {
	email = normaliseEmail(email)
	if !em.isRecipient(email) {
		em.To = append(em.To, email)
		em.addRecipient(email)
	}
}

// AddCcRecipient directly adds someone to the CC list.
// Emails are normalised before addition or removal.
func (em *Email) AddCcRecipient(email string) {
	email = normaliseEmail(email)
	if !em.isRecipient(email) {
		em.Cc = append(em.Cc, email)
		em.addRecipient(email)
	}
}

// AddBccRecipient directly adds someone to the BCC list.
// Emails are normalised before addition or removal.
func (em *Email) AddBccRecipient(email string) {
	email = normaliseEmail(email)
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
	email = normaliseEmail(email)
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
