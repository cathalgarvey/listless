package main

import (
	"encoding/json"
	"errors"
	"time"

	"gopkg.in/inconshreveable/log15.v2"

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
		Email:       normaliseEmail(usremail),
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
		log15.Error("Error in IsAllowedPost getting subscriber", log15.Ctx{"context": "db", "error": err})
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
		members := tx.Bucket([]byte(memberBucketName))
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
		members := tx.Bucket([]byte(memberBucketName))
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
		members := tx.Bucket([]byte(memberBucketName))
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
		members := tx.Bucket([]byte(memberBucketName))
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
		log15.Error("Error in goGetAllSubscribers", log15.Ctx{"context": "db", "error": err})
	}
	return subscribers
}

// This is a function that can iterate over members to gather data.
type subscriberViewF func(email string, meta *MemberMeta) error

// A read-only iteration over the members in the database. Faster and safer than forEachSubscriberRW
func (db *ListlessDB) forEachSubscriber(viewer subscriberViewF) error {
	return db.View(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte(memberBucketName))
		return members.ForEach(func(email_b, meta_b []byte) error {
			oldemail := string(email_b)
			meta := MemberMeta{}
			err := json.Unmarshal(meta_b, &meta)
			if err != nil {
				return err
			}
			return viewer(oldemail, &meta)
		})
	})
}

// This is a function that can be used to iterate over members and optionally make
// changes to them.
type subscriberUpdateF func(email string, meta *MemberMeta) (edit bool, newemail string, newmeta *MemberMeta, err error)

// A RW iteration over subscribers. If the provided function returns edit=false, then
// no changes are made (a read only operation). In this case, the iteration is
// safe and the database will be guaranteed consistent, within Bolt's usual guarantees.
// If, however, it provides edit=true, then the following rules apply:
// * If the returned MemberMeta is nil, then the original entry is deleted.
// * If the returned MemberMeta is not nil, and the returned string is empty,
//   then the data for the selected user is modified in-place in the database.
// * If the returned MemberMeta is not nil, and the returned string is non-empty,
//   then the original data is deleted and the new MemberMeta is entered under
//   the new string key (expected to be an email address, as usual).
// Please note: The above operations are queued during iteration but do not
// take place until afterwards, as they must get a lock on the database. This
// means that forEachSubscriber is not a safe operation if the database might
// get interrupted; it is built for convenience, not safety!
func (db *ListlessDB) forEachSubscriberRW(updater subscriberUpdateF) error {
	return db.Update(func(tx *bolt.Tx) error {
		members := tx.Bucket([]byte(memberBucketName))
		return members.ForEach(func(email_b, meta_b []byte) error {
			oldemail := string(email_b)
			meta := MemberMeta{}
			err := json.Unmarshal(meta_b, &meta)
			if err != nil {
				return err
			}
			edit, newemail, newmeta, err := updater(oldemail, &meta)
			if err != nil {
				return err
			}
			if !edit {
				return nil
			}
			if newmeta == nil {
				// Delete original entry. This spins up a goroutine that will wait for an Update tx.
				go db.DelSubscriber(oldemail)
				return nil
			} else {
				// Edit original entry. This may involve scheduling a deletion.
				if newemail != "" {
					// spin up a delete for the old entry and an add for the new entry.
					// Both will await their turn so the database could get screwed during
					// these ops.
					go db.DelSubscriber(oldemail)
					go db.UpdateSubscriber(newemail, newmeta)
					return nil
				} else {
					go db.UpdateSubscriber(oldemail, newmeta)
				}
			}
			return nil
		})
	})
}
