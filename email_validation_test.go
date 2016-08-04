package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNormalise(t *testing.T) {
	assert.Equal(t, "cathal@garvey.me", normaliseEmail("cathal@garvey.me"))
	assert.Equal(t, "cathal@garvey.me", normaliseEmail("Cathal@garvey.me"))
	assert.Equal(t, "cathal@formalabs.org", normaliseEmail("cathal@formalabs.org"))
}
