package main

import (
	"log"

	"github.com/dchest/validator"
	"github.com/jordan-wright/email"
)

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
	newe.Email = e
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
			log.Println("Skipping recipient as it's already been seen: " + e)
			continue
		} else {
			em.inRecipientLists[e] = struct{}{}
		}
		newTo = append(newTo, e)
	}
	// Overwrites?
	em.To = append(em.To[:1], newTo...)

	newCc := make([]string, 0, len(em.Cc))
	for _, e := range em.Cc {
		e = validator.NormalizeEmail(e)
		if _, ok := em.inRecipientLists[e]; ok {
			log.Println("Skipping recipient as it's already been seen: " + e)
			continue
		} else {
			em.inRecipientLists[e] = struct{}{}
		}
		newCc = append(newCc, e)
	}
	em.Cc = append(em.Cc[:len(em.Cc)], newCc...)

	newBcc := make([]string, 0, len(em.Bcc))
	for _, e := range em.Bcc {
		e = validator.NormalizeEmail(e)
		if _, ok := em.inRecipientLists[e]; ok {
			log.Println("Skipping recipient as it's already been seen: " + e)
			continue
		} else {
			em.inRecipientLists[e] = struct{}{}
		}
		newBcc = append(newBcc, e)
	}
	em.Bcc = append(em.Bcc[:len(em.Bcc)], newBcc...)
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
