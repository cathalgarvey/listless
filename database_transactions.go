package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/boltdb/bolt"
)

var (
	// ErrTransactionNotReady is returned when fields are missing from MailTransactions
	ErrTransactionNotReady = errors.New("Transaction does not have all required fields")
	// ErrExpiredTransaction is returned when a transaction is invalid due to expiry time/date.
	ErrExpiredTransaction = errors.New("This transaction has expired and cannot be used")
	// ErrTransactionNotFound is returned when a secret fails to yield a transaction item in the database.
	// This may be due to expiry or nonexistence.
	ErrTransactionNotFound = errors.New("Provided transaction secret did not yield a transaction item; nonexistent or expired and cleared out?")
)

// MailTransaction is the unit of authentication for mailing list subscriptions,
//  unsubscriptions and moderator commands. It is a structure containing the hash
//  of a private value that must be embedded in the subject line, a list of email
//  addresses from which a call will be trusted, and a reference code that may be
//  used to represent a "KV" bucket key (the bucket itself is assumed to be implied
//  by the operation).
// MailTransaction is not directly exposed to Lua. A mailtransaction can be created from Lua and
//  triggered from Lua, but all trigger information must be within the ScriptName, ScriptHook and RefCode
//  parameters.
type MailTransaction struct {
	// Refcode is a string value that can be set and stored by the
	// initiating script for this transaction. It is intended to be used
	// as a bucket key for whatever bucket the transaction script uses.
	// It can, however, be whatever the script likes including JSON.
	RefCode string
	// The name of the script to call with this transaction when invoked.
	ScriptName string
	// The name of the hook function in the script to call when this is invoked.
	// This allows scripts to be easily written for hooking multiple transactions,
	// so for example a subscription-by-mail handler could have a dispatch hook
	// that parses incoming mails for messages from would-be-(un)subscribers,
	// sets up either a "subscribe" transaction or an "unsubscribe" transaction,
	// which behave identically except their hooks have different names in the same
	// script; one adds a subscriber, one removes the subscriber.
	ScriptHook string
	// Emails that are permitted to trigger this tranaction. If empty, anyone can.
	Permitted []string
	// When this job expires, and can be deleted by the calling script.
	// A function is provided in Lua scope to fetch all refcodes for expired
	// transactions, so that implementing scripts can clean up their buckets.
	Expires time.Time
	// Can this transaction be triggered more than once?
	Persists bool
}

// Validate a MailTransaction as having the required fields before database insertion.,
// and normalise all email addresses in permitted field.
func (trans *MailTransaction) prepare() error {
	if trans.isExpired() {
		return ErrExpiredTransaction
	}
	if trans.ScriptHook == "" || trans.ScriptName == "" {
		return ErrTransactionNotReady
	}
	for i := 0; i < len(trans.Permitted); i++ {
		trans.Permitted[i] = normaliseEmail(trans.Permitted[i])
	}
	return nil
}

// Validate makes the various checks that a transaction is permissible:
// * Email is one of the values in "Permitted" (after normalisation)
// * Expires is not yet past
// NB: Secret may be part of overall authentication but it's not visible to MailTransaction structs
// and is presumed checked already by the time one is fetched.
func (trans *MailTransaction) Validate(email *Email) bool {
	return trans.isPermitted(email.Sender) && (!trans.isExpired())
}

// Check if the transaction expiry time is after now.
func (trans *MailTransaction) isExpired() bool {
	return time.Now().After(trans.Expires)
}

// Is sender email address permitted to trigger this Transaction
func (trans *MailTransaction) isPermitted(emailAddr string) bool {
	emailAddr = normaliseEmail(emailAddr)
	for _, pEmail := range trans.Permitted {
		if emailAddr == pEmail {
			return true
		}
	}
	return false
}

// GetTransaction takes a parsed secret, hashes it to locate a current Transaction,
// and returns it to the caller. If none is found, it returns nil.
// If the transaction is expired, it also returns nil and the transaction is
// deleted, but an ErrExpiredTransaction error will be returned; this can be
// identified to send an expiry notice to the caller, if desired.
func (db *ListlessDB) GetTransaction(secret string) (trans *MailTransaction, err error) {
	sHash := hashSecret(secret)
	err = db.View(func(tx *bolt.Tx) error {
		transBucket := tx.Bucket([]byte(transactionBucketName))
		v := transBucket.Get(sHash)
		if v == nil {
			return ErrTransactionNotFound
		}
		return json.Unmarshal(v, trans)
	})
	if err != nil {
		return nil, err
	}
	return trans, err
}

// PutTransaction takes a secret, hashes it to create a bucket key,
// and stores the provided data in the database. This function is fussy
// about certain fields of MailTransaction and will require them to be non-zero-value;
// for example, a MailTransaction without both a ScriptName and ScriptHook value
// cannot be dispatched to a handler!
func (db *ListlessDB) PutTransaction(secret string, newTransaction *MailTransaction) error {
	if err := newTransaction.prepare(); err != nil {
		return err
	}
	sHash := hashSecret(secret)
	jTransaction, err := json.Marshal(newTransaction)
	if err != nil {
		return err
	}
	return db.Update(func(tx *bolt.Tx) error {
		transBucket := tx.Bucket([]byte(transactionBucketName))
		return transBucket.Put(sHash, jTransaction)
	})
}

// RegisterTransaction is exposed in Lua. It is how new transactions are created and stored.
func (db *ListlessDB) RegisterTransaction(secret, scriptname, scripthook, refcode string, permitted []string, validhours int, persists bool) error {
	newTransaction := MailTransaction{
		ScriptName: scriptname,
		ScriptHook: scripthook,
		RefCode:    refcode,
		Permitted:  permitted,
		Expires:    time.Now().Add(time.Duration(validhours) * time.Hour),
		Persists:   persists,
	}
	return db.PutTransaction(secret, &newTransaction)
}

// HasTransaction is exposed in Lua. It accepts a secret value and returns true if it exists, but does
// not trigger it.
func (db *ListlessDB) HasTransaction(secret string) bool {
	return false
}

// TriggerTransaction is exposed in Lua. It is how new transactions are searched for and triggered.
// This is a simultaneous check-and-trigger; for a check only use HasTransaction.
// This calls the Transaction's script and hook with the database, the email struct, and the refcode.
// The hook may return an abitrary string which is returned to Lua, and an arbitrary string which is
// converted to an error on the way out of TriggerTransaction. In turn, the triggering script will
// receive (hookReturnedString, transactionRefcode, error), all strings or nil.
func (db *ListlessDB) TriggerTransaction(secret string, email *Email) (hookreturnvalue, refcode string, err error) {
	// Get transaction
	// Validate transaction
	// Trigger transaction
	// If transaction is expired, delete transaction
	// Return refcode so script can clean up
	return hookreturnvalue, refcode, nil
}

// sha256 the secret to get the hash. May change in future to some other function;
// deliberately partitioned for modularity.
func hashSecret(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}
