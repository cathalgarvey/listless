package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/boltdb/bolt"
	"github.com/layeh/gopher-luar"
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

// ListlessDB - The database object as injected into the Lua eventloop. Thanks
// to luar, the below methods are available in the Lua runtime, though argument
// types include a custom struct, MemberMeta, which means modifying member
// details will likely require creating an empty member with database:AddSubscriber("foo@bar.com", nil),
// fetching the new entry with `local foometa = database:GetSubscriber("foo@bar.com"), modifying the returned
// value, and then using database:AddSubscriber("foo@bar.com", foometa) to overwrite.
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
		log.Printf("Error in goGetAllSubscribers: %s", err.Error())
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
		log.Println("Error creating KV store (returning nil): ", err)
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
		log.Println("Store operation called on destroyed bucket. Ignoring, but check your code!")
		return
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.Put([]byte(key), []byte(value))
	})
	if err != nil {
		log.Println("Error storing value in KV bucket: ", err)
	}
}

// Retrieve a string value for a string key. Returns empty string on failure.
func (kv *ListlessKVStore) Retrieve(key string) string {
	if kv.destroyed {
		log.Println("Retrieve operation called on destroyed bucket. Ignoring, but check your code!")
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
		log.Println("Error retrieving key from KV bucket (returning empty string): ", err)
		return ""
	}
	return value
}

// Delete a value associated with a key in this KV store. No error if absent.
func (kv *ListlessKVStore) Delete(key string) {
	if kv.destroyed {
		log.Println("Delete operation called on destroyed bucket. Ignoring, but check your code!")
		return
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte("kvstores"))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.Delete([]byte(key))
	})
	if err != nil {
		log.Println("Error deleting key from KV bucket: ", err)
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
		log.Println("Error iterating over keys in a bucket to return key-list: ", err)
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
		log.Println("Error destroying bucket: ", err)
	}
}

// itob returns an 8-byte big endian representation of v.
func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}
