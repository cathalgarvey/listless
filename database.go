package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"time"

	"github.com/boltdb/bolt"
	"github.com/layeh/gopher-luar"
	"github.com/yuin/gopher-lua"
)

var (
	// ErrInvalidEmail - Returned when adding a member fails because email address
	// is invalid.
	ErrInvalidEmail = errors.New("Invalid email given, cannot add subscriber")

	// ErrMemberEntryNotFound - Returned when an email has no database entry
	ErrMemberEntryNotFound = errors.New("Member entry not found by provided email")

	// ErrMemberBucketNotFound - Returned when a database lookup fails at the bucket level.
	ErrMemberBucketNotFound = errors.New("Member bucket not found")

	// ErrArchiveBucketNotFound - Returned when a database lookup fails at the bucket level.
	ErrArchiveBucketNotFound = errors.New("Archive bucket not found")
)

// MemberMeta is the database representation of a subscriber.
// This is all pretty pedestrian but note that "Joindate" is a Go time object,
// so consult the documentation for how to extract data using time methods.
type MemberMeta struct {
	Joindate    time.Time
	Moderator   bool
	AllowedPost bool
	Name        string
	Email       string
}

// CreateSubscriber - Create a new Subscriber. It is not added to the database.
// This is used to create a Meta object, and may be updated to include any new
// keys in the MemberMeta object such as may be added.
func (db *ListlessDB) CreateSubscriber(usremail, usrname string, allowedpost, moderator bool) *MemberMeta {
	m := MemberMeta{
		Joindate:    time.Now().Round(time.Hour),
		Moderator:   moderator,
		AllowedPost: allowedpost,
		Name:        usrname,
		Email:       usremail,
	}
	return &m
}

// SetJoinDateUTC - Modify Joindate to a manually set date in UTC.
// If stupid values are given they will be normalised by the Go time API without
// creating an error. This may result in stupid database entries. Months are indexed from
// 1, not zero, so January is 1, February is 2, December is 12.
func (m *MemberMeta) SetJoinDateUTC(year, month, day, hour int) {
	m.Joindate = time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
}

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
		if _, err := tx.CreateBucketIfNotExists([]byte("members")); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte("kvstores")); err != nil {
			return err
		}
		return nil
	})
}

// IsModerator - Fetch a subscriber and return whether the "Moderator" flag is true.
// For unknown addresses the answer is always false.
// On error, returns false.
func (db *ListlessDB) IsModerator(email string) bool {
	sub, err := db.GetSubscriber(email)
	if err != nil {
		return false
	}
	return sub.Moderator
}

// IsAllowedPost - Fetch a subscriber and return whether the "AllowedPost" flag is true.
// For unknown addresses the answer is always false.
// On error, returns false.
func (db *ListlessDB) IsAllowedPost(email string) bool {
	sub, err := db.GetSubscriber(email)
	if err != nil {
		return false
	}
	return sub.AllowedPost
}

// GetSubscriber - Normalise email and fetch subscriber meta, if any.
func (db *ListlessDB) GetSubscriber(email string) (*MemberMeta, error) {
	email = normaliseEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	sub := MemberMeta{}
	err := db.View(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		if members == nil {
			return ErrMemberBucketNotFound
		}
		mementry := members.Get([]byte(email))
		if mementry == nil {
			return ErrMemberEntryNotFound
		}
		return json.Unmarshal(mementry, &sub)
	})
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// UpdateSubscriber - Validate an email address and store a MemberMeta record in
//  the database. This can be used to create or update a member. To obtain the
// meta object, either use GetSubscriber or use CreateSubscriber.
func (db *ListlessDB) UpdateSubscriber(usremail string, meta *MemberMeta) error {
	usremail = normaliseEmail(usremail)
	if usremail == "" {
		return ErrInvalidEmail
	}
	return db.Update(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		if members == nil {
			return ErrMemberBucketNotFound
		}
		mementry, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return members.Put([]byte(usremail), mementry)
	})
}

// DelSubscriber - Delete a subscriber. Returns no error if subscriber didn't exist.
func (db *ListlessDB) DelSubscriber(email string) error {
	email = normaliseEmail(email)
	if email == "" {
		return ErrInvalidEmail
	}
	return db.Update(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		if members == nil {
			return ErrMemberBucketNotFound
		}
		return members.Delete([]byte(email))
	})
}

// TODO: Do away with this "true for moderators" crap and let people iterate in Lua
// if they want only moderators.

// GetAllSubscribers - This returns a *lua Table* consisting of all subscriber email addresses.
func (db *ListlessDB) GetAllSubscribers(L *luar.LState) int {
	mo := false
	// CheckBool appears to check the type of the indexed item and returns false
	// by default if it's (absent or?) not a bool, but if it *is* it returns the value.
	// So, the below should eval "true" if a lua "true" is passed, false otherwise.
	// TODO: Should the stack be popped of the arg?
	// This suggests not unless accessing globals: https://stackoverflow.com/questions/1217423/how-to-use-lua-pop-function-correctly
	if L.CheckBool(1) {
		mo = true
	}
	// Get the subscriber list..
	subs := db.goGetAllSubscribers(mo)
	// L.CreateTable does *not* appear to push table onto stack during creation.
	// The args are the preallocated "listy" allocations and the "tabular" allocs.
	T := L.CreateTable(len(subs), 0)
	for _, sub := range subs {
		// Need to explicitly pass lua.LState rather than luar.LState..
		T.Append(luar.New(L.LState, sub))
	}
	L.Push(T)
	return 1
}

// GetAllSubscribers - Return a slice of all member emails.
// The variadic modsOnly argument is used in order to allow argumentless use
// within Lua; all booleans after the first are ignored.
func (db *ListlessDB) goGetAllSubscribers(modsOnly bool) (subscribers []string) {
	subscribers = make([]string, 0)
	err := db.View(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		return members.ForEach(func(email, metabytes []byte) error {
			meta := MemberMeta{}
			err := json.Unmarshal(metabytes, &meta)
			if err != nil {
				return err
			}
			if modsOnly && (!meta.Moderator) {
				return nil
			}
			subscribers = append(subscribers, meta.Email)
			return nil
		})
	})
	if err != nil {
		dbLog.Error("Error in goGetAllSubscribers: %s", err.Error())
	}
	return subscribers
}

// KVStore creates or fetches a key:value bucket in kvbuckets and returns it in Lua.
func (db *ListlessDB) KVStore(bucketName string) *ListlessKVStore {
	kv := &ListlessKVStore{
		parentDB:   db,
		BucketName: bucketName,
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		kvbucket.CreateBucketIfNotExists([]byte(kv.BucketName))
		return nil
	})
	if err != nil {
		dbLog.Error("Error creating KV store (returning nil): %s", err.Error())
		return nil
	}
	return kv
}

// ListlessKVStore is the Lua representation of a Bolt bucket, and offers easy
// means to set, get, and delete values in a simple KV store for persistent
// string:string mappings.
type ListlessKVStore struct {
	parentDB   *ListlessDB
	destroyed  bool
	BucketName string
}

// Store a string->string mapping in this kv store. Replaces any prior value.
func (kv *ListlessKVStore) Store(key, value string) {
	if kv.destroyed {
		luaLog.Error("Store operation called on destroyed bucket: %s", kv.BucketName)
		return
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.Put([]byte(key), []byte(value))
	})
	if err != nil {
		dbLog.Error("Error storing value in KV bucket: %s", err.Error())
	}
}

// Retrieve a string value for a string key. Returns empty string on failure.
func (kv *ListlessKVStore) Retrieve(key string) string {
	if kv.destroyed {
		luaLog.Error("Retrieve operation called on destroyed bucket: %s", kv.BucketName)
		return ""
	}
	// TODO: Tidy this up for errors where bucket retrieval goes awry..
	var value string
	err := kv.parentDB.View(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		valb := bucket.Get([]byte(key))
		value = string(valb)
		return nil
	})
	if err != nil {
		dbLog.Error("Error retrieving key from KV bucket (returning empty string): %s", err.Error())
		return ""
	}
	return value
}

// Delete a value associated with a key in this KV store. No error if absent.
func (kv *ListlessKVStore) Delete(key string) {
	if kv.destroyed {
		luaLog.Error("Delete operation called on destroyed bucket: %s", kv.BucketName)
		return
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.Delete([]byte(key))
	})
	if err != nil {
		dbLog.Error("Error deleting key from KV bucket: %s", err.Error())
	}
}

// Keys - Return a list-like table of all keys currently in the KV store.
func (kv *ListlessKVStore) Keys(L *luar.LState) int {
	var keys []string
	err := kv.parentDB.View(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.ForEach(func(k, v []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})
	if err != nil {
		dbLog.Error("Error iterating over keys in a bucket to return key-list: %s", err.Error())
		return 0
	}
	T := L.CreateTable(len(keys), 0)
	for _, k := range keys {
		// Need to explicitly pass lua.LState rather than luar.LState..
		T.Append(luar.New(L.LState, k))
	}
	L.Push(T)
	return 1
}

// Destroy deletes a bucket from the KV store backend, and marks it as destroyed
// so any methods called on remaining instances of the ListlessKVStore object will
// fail without corrupting the database.
func (kv *ListlessKVStore) Destroy() {
	kv.destroyed = true
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		return kvbucket.DeleteBucket([]byte(kv.BucketName))
	})
	if err != nil {
		dbLog.Error("Error destroying bucket: %s", err.Error())
	}
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
