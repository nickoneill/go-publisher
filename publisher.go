package main

import (
	// "github.com/garyburd/go-oauth"
	"github.com/nickoneill/go-dropbox"
)

func main() {
	_ = dropbox.NewClient("key", "secret")
}