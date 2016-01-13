package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/boltdb/bolt"
	"github.com/jordan-wright/email"
)

// MemberMeta is the database representation of a subscriber.
// This is all pretty pedestrian but note that "Joindate" is a Go time object,
// so consult the documentation for how to extract data using time methods.
type MemberMeta struct {
	Joindate  time.Time
	Moderator bool
	Name      string
	Email     string
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
	} else if len(boltconf) == 1 {
		db, err = bolt.Open(loc, 0600, boltconf[0])
	} else {
		return nil, errors.New("Only one bolt.Options instance should be provided.")
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
		if _, err := tx.CreateBucketIfNotExists([]byte("archive")); err != nil {
			return err
		}
		return nil
	})
}

// StoreEmail - Store a message to the archive *after* processing. Message is
// stored sequentially, so the archive is chronological.
func (db *ListlessDB) StoreEmail(e *email.Email) error {
	return db.Update(func(tx *bolt.Tx) error {
		archive := tx.Bucket([]byte("archive"))
		if archive == nil {
			return errors.New("Bucket 'archive' not found!")
		}
		key, err := archive.NextSequence()
		if err != nil {
			return err
		}
		value, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return archive.Put(itob(int(key)), value)
	})
}

// GetSubscriber - Normalise email and fetch subscriber meta, if any.
func (db *ListlessDB) GetSubscriber(email string) (*MemberMeta, error) {
	email = normaliseEmail(email)
	if email == "" {
		return nil, errors.New("Invalid email given, cannot fetch subscriber.")
	}
	sub := MemberMeta{}
	err := db.View(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		if members == nil {
			return errors.New("Bucket 'members' not found!")
		}
		mementry := members.Get([]byte(email))
		if mementry == nil {
			return errors.New("No database entry found for member '" + email + "'")
		}
		return json.Unmarshal(mementry, &sub)
	})
	if err != nil {
		return nil, err
	}
	return &sub, nil
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

// AddSubscriber - Validate a new email address and store a MemberMeta record in
//  the database.
// If given, meta is used, so this can be used to update records.
// If not given, a new meta is created, with a blank name and the current time.
// Emails are used as keys, with MemberMeta objects being stored as values.
// This will return an error if an email cannot be validated successfully.
func (db *ListlessDB) AddSubscriber(email string, meta *MemberMeta) error {
	email = normaliseEmail(email)
	if email == "" {
		return errors.New("Invalid email given, cannot add subscriber.")
	}
	if meta == nil {
		meta = &MemberMeta{
			Joindate:  time.Now().Round(time.Hour),
			Moderator: false,
			Name:      "",
			Email:     email,
		}
	}
	return db.Update(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		if members == nil {
			return errors.New("Bucket 'members' not found!")
		}
		mementry, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return members.Put([]byte(email), mementry)
	})
}

// DelSubscriber - Delete a subscriber. Returns no error if subscriber didn't exist.
func (db *ListlessDB) DelSubscriber(email string) error {
	email = normaliseEmail(email)
	if email == "" {
		return errors.New("Invalid email given, cannot delete subscriber.")
	}
	return db.Update(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		if members == nil {
			return errors.New("Bucket 'members' not found!")
		}
		return members.Delete([]byte(email))
	})
}

// GetAllSubscribers - Return a slice of all member emails.
// The variadic modsOnly argument is used in order to allow argumentless use
// within Lua; all booleans after the first are ignored.
func (db *ListlessDB) GetAllSubscribers(modsOnly ...bool) (subscribers []string) {
	mo := false
	if len(modsOnly) > 0 {
		if modsOnly[0] {
			mo = true
		}
	}
	subscribers = make([]string, 0)
	db.View(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte("members"))
		c := members.Cursor()
		for email, metabytes := c.First(); email != nil; email, _ = c.Next() {
			meta := MemberMeta{}
			err := json.Unmarshal(metabytes, &meta)
			if err != nil {
				log.Printf("Error unmarshalling membermeta for user '%s': %s (meta was: %v)", string(email), err.Error(), metabytes)
				continue
			}
			if mo && (!meta.Moderator) {
				continue
			}
			subscribers = append(subscribers, meta.Email)
		}
		return nil
	})
	return subscribers
}

// itob returns an 8-byte big endian representation of v.
func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}
