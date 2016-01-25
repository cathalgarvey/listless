package main

import (
	"encoding/binary"
	"errors"
	"io/ioutil"
	"os"

	"github.com/boltdb/bolt"
	"github.com/layeh/gopher-luar"
	"github.com/yuin/gopher-lua"
)

var (
	// ErrMemberBucketNotFound - Returned when a database lookup fails at the bucket level.
	ErrMemberBucketNotFound = errors.New("Member bucket not found")

	// ErrArchiveBucketNotFound - Returned when a database lookup fails at the bucket level.
	ErrArchiveBucketNotFound = errors.New("Archive bucket not found")

	bucketList = []string{"members", "kvstores", "transactions"}
)

// ListlessDB - The database object used by Listless. This wraps boltdb and adds
// extra methods for handling memberships and K/V bucket datastores.
// This is never directly injected into Lua, but is further wrapped in either
// PrivilegedDBWrapper or ModeratorDBWrapper, which have whitelisted methods
// appropriate to their execution contexts.
type ListlessDB struct {
	*bolt.DB
}

// NewDatabase - Open a Bolt DB optionally with a Bolt Options instance.
func NewDatabase(loc string, boltconf ...*bolt.Options) (ldb *ListlessDB, err error) {
	var db *bolt.DB
	ldb = &ListlessDB{}
	if len(boltconf) == 0 {
		db, err = bolt.Open(loc, 0600, nil)
	} else {
		db, err = bolt.Open(loc, 0600, boltconf[0])
	}
	if err != nil {
		return nil, err
	}
	// Configure database buckets.
	ldb.DB = db
	return ldb, db.Update(func(tx *bolt.Tx) error {
		for _, bucketName := range bucketList {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucketName)); err != nil {
				return err
			}
		}
		return nil
	})
}

// itob returns an 8-byte big endian representation of v.
func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

// Create a temporary Boltdb to register whitelisted methods in this Lua state
// using Luar, then destroy the temporary boltdb once it's finished.
func applyLuarWhitelists(L *lua.LState) error {
	dummyf, err := ioutil.TempDir("", "listless")
	if err != nil {
		return err
	}
	// Destroy temporary directory and contents when finished.
	defer os.RemoveAll(dummyf)
	// Make DB in tempdir just to register and get a Metatable from Luar.
	dummydb, err := NewDatabase(dummyf + "tmpdb.db")
	if err != nil {
		return err
	}
	// Whitelist Nothing for a bare database!
	luar.MT(L, dummydb).Whitelist()
	// Apply levelled whitelists for "Privileged" (full access) and
	// "Moderator" (limited access) database wrappers.
	privDBMT := luar.MT(L, dummydb.PrivilegedDBWrapper())
	privDBMT.Whitelist(PrivilegedDBPermittedMethods...)
	modrDBMT := luar.MT(L, dummydb.ModeratorDBWrapper())
	modrDBMT.Whitelist(ModeratorDBPermittedMethods...)
	// Must also whitelist the Key/Value stores to prevent access to underlying DB.
	dummykv := dummydb.KVStore("dummy")
	kvMT := luar.MT(L, dummykv)
	kvMT.Whitelist(ListlessKVStorePermittedMethods...)
	return nil
}

// PrivilegedDBWrapper is a struct embedding ListlessDB which is used in PrivilegedSandbox
// and has a luar metatable permitting all of the ListlessDB methods, but no boltdb
// methods.
type PrivilegedDBWrapper struct {
	*ListlessDB
}

// PrivilegedDBWrapper is used when inserting a database into Lua to help luar pick
// which metatable to attach for security's sake.
func (db *ListlessDB) PrivilegedDBWrapper() *PrivilegedDBWrapper {
	ndb := new(PrivilegedDBWrapper)
	ndb.ListlessDB = db
	return ndb
}

// ModeratorDBWrapper is a struct embedding ListlessDB which is used in PrivilegedSandbox
// and has a luar metatable permitting all of the ListlessDB methods, but no boltdb
// methods.
type ModeratorDBWrapper struct {
	*ListlessDB
}

// ModeratorDBWrapper is used when inserting a database into Lua to help luar pick
// which metatable to attach for security's sake.
func (db *ListlessDB) ModeratorDBWrapper() *ModeratorDBWrapper {
	ndb := new(ModeratorDBWrapper)
	ndb.ListlessDB = db
	return ndb
}

// PrivilegedDBPermittedMethods is a list of permitted fields/methods on a PrivilegedDBWrapper
// within Lua.
var PrivilegedDBPermittedMethods = []string{
	"IsModerator", "IsAllowedPost",
	"CreateSubscriber", "UpdateSubscriber", "DelSubscriber",
	"GetAllSubscribers", "KVStore",
}

// ModeratorDBPermittedMethods is a list of permitted fields/methods on a ModeratorDBWrapper
// within Lua.
var ModeratorDBPermittedMethods = []string{
	"IsModerator", "IsAllowedPost",
	"CreateSubscriber", "UpdateSubscriber", "GetSubscriber", "DelSubscriber",
	// Getting subscriber list is not permitted for Moderators, as they can always
	// GetSubscriber using a known email address.
	// Moderators are also not currently given KVStore access.
}

// ListlessKVStorePermittedMethods - Whitelisted fields/methods for the ListlessKVStore type in luar.
var ListlessKVStorePermittedMethods = []string{
	"Store", "Retrieve", "Delete", "Keys", "Destroy", "BucketName",
}
