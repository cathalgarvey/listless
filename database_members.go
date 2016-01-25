package main

import (
	"encoding/json"
	"errors"
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
