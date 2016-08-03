package main

import (
	"github.com/boltdb/bolt"
	"github.com/layeh/gopher-luar"
	"gopkg.in/inconshreveable/log15.v2"
)

// ListlessKVStore is the Lua representation of a Bolt bucket, and offers easy
// means to set, get, and delete values in a simple KV store for persistent
// string:string mappings.
type ListlessKVStore struct {
	parentDB   *ListlessDB
	destroyed  bool
	BucketName string
}

// KVStore creates or fetches a key:value bucket in kvbuckets and returns it in Lua.
func (db *ListlessDB) KVStore(bucketName string) *ListlessKVStore {
	kv := &ListlessKVStore{
		parentDB:   db,
		BucketName: bucketName,
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte(kvBucketName))
		kvbucket.CreateBucketIfNotExists([]byte(kv.BucketName))
		return nil
	})
	if err != nil {
		log15.Error("Error creating KV store (returning nil)", log15.Ctx{"context": "db", "error": err})
		return nil
	}
	return kv
}

// Store a string->string mapping in this kv store. Replaces any prior value.
func (kv *ListlessKVStore) Store(key, value string) {
	if kv.destroyed {
		log15.Error("Store operation called on destroyed bucket", log15.Ctx{"context": "lua", "bucket": kv.BucketName})
		return
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte(kvBucketName))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.Put([]byte(key), []byte(value))
	})
	if err != nil {
		log15.Error("Error storing value in KV bucket", log15.Ctx{"context": "db", "error": err})
	}
}

// Retrieve a string value for a string key. Returns empty string on failure.
func (kv *ListlessKVStore) Retrieve(key string) string {
	if kv.destroyed {
		log15.Error("Retrieve operation called on destroyed bucket", log15.Ctx{"context": "db", "bucket": kv.BucketName})
		return ""
	}
	// TODO: Tidy this up for errors where bucket retrieval goes awry..
	var value string
	err := kv.parentDB.View(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte(kvBucketName))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		valb := bucket.Get([]byte(key))
		value = string(valb)
		return nil
	})
	if err != nil {
		log15.Error("Error retrieving key from KV bucket (returning empty string)", log15.Ctx{"context": "db", "error": err})
		return ""
	}
	return value
}

// Delete a value associated with a key in this KV store. No error if absent.
func (kv *ListlessKVStore) Delete(key string) {
	if kv.destroyed {
		log15.Error("Delete operation called on destroyed bucket", log15.Ctx{"context": "db", "bucket": kv.BucketName})
		return
	}
	err := kv.parentDB.Update(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte(kvBucketName))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.Delete([]byte(key))
	})
	if err != nil {
		log15.Error("Error deleting key from KV bucket", log15.Ctx{"context": "db", "error": err})
	}
}

// Keys - Return a list-like table of all keys currently in the KV store.
func (kv *ListlessKVStore) Keys(L *luar.LState) int {
	var keys []string
	err := kv.parentDB.View(func(tx *bolt.Tx) error {
		kvbucket := tx.Bucket([]byte(kvBucketName))
		bucket := kvbucket.Bucket([]byte(kv.BucketName))
		return bucket.ForEach(func(k, v []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})
	if err != nil {
		log15.Error("Error iterating over keys in a bucket to return key-list", log15.Ctx{"context": "db", "error": err})
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
		kvbucket := tx.Bucket([]byte(kvBucketName))
		return kvbucket.DeleteBucket([]byte(kv.BucketName))
	})
	if err != nil {
		log15.Error("Error destroying bucket", log15.Ctx{"context": "db", "error": err})
	}
}
