package main

import (
	"fmt"
	"time"
	// "github.com/garyburd/go-oauth"
	// "github.com/nickoneill/go-dropbox"
	// "launchpad.net/goyaml"
)

type Chunk struct {
	Command string
}

// main just loops and waits for jobs to return a command on a channel
func main() {
	callback := make(chan *Chunk)
	go dropboxscrape(callback)
	// add new jobs here

	for {
		fmt.Printf("starting loop\n")
		
		chunk := <-callback
		switch chunk.Command {
		case "republish":
			go rebuildSite()
		case "somethingelse":
			go doSomething()
		}
	}
}

// this is the core job, it checks for changes in the dropbox app folder
// every n minutes and generates a new site if there are changes
func dropboxscrape(back chan *Chunk) {
	for {
		fmt.Printf("starting dropbox check\n")

		back <- &Chunk{Command: "republish"}

		fmt.Printf("sleeping dropbox check\n")
		time.Sleep(10*time.Second)
	}
}

// basic rebuild command, builds site from dropbox files and deploys to configured location
func rebuildSite() {
	fmt.Printf("rebuilding\n")
}

func doSomething() {
	fmt.Printf("never get here")
}