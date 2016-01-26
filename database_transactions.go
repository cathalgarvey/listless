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
type MailTransaction struct {
	// Emails that are permitted to trigger this tranaction. If empty, anyone can.
	Permitted []string
	// Can this transaction be triggered more than once?
	Persists bool
	// Refcode is a string value that can be set and stored by the
	// initiating script for this transaction. It is intended to be used
	// as a bucket key for whatever bucket the transaction script uses.
	// It can, however, be whatever the script likes including JSON.
	RefCode string
	// When this job expires, and can be deleted by the calling script.
	// A function is provided in Lua scope to fetch all refcodes for expired
	// transactions, so that implementing scripts can clean up their buckets.
	Expires time.Time
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
// (It is assumed by now that secret value has been checked because it's
// supposed to be required to find transactions)
func (trans *MailTransaction) Validate(email *Email) bool {
	if trans.isExpired() {
		return false
	}
	if trans.isPermitted(email.Sender) {
		return true
	}
	return false
}

func (trans *MailTransaction) isExpired() bool {
	return time.Now().After(trans.Expires)
}

// Is sender email address permitted to trigger this Transaction
func (trans *MailTransaction) isPermitted(email string) bool {
	email = normaliseEmail(email)
	for _, pEmail := range trans.Permitted {
		if email == pEmail {
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

// sha256 the secret to get the hash. May change in future to some other function;
// deliberately partitioned for modularity.
func hashSecret(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}
