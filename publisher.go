package main

import (
	"fmt"
	"time"
	"io/ioutil"
	"net/http"
	"encoding/json"
	"strings"
	// "path/filepath"
	// "os"
	"github.com/garyburd/go-oauth"
	"github.com/nickoneill/go-dropbox"
	// "launchpad.net/goyaml"
)

const app_key = "ylg2zoaj78ol2dz"
const app_secret = "i2863bf9odkbdl7"
const callback_url = "http://www.someurl.com/callback"

var (
	db = dropbox.NewClient(app_key, app_secret)
	creds = new(oauth.Credentials)
)

type Chunk struct {
	Command string
}

// main just loops and waits for jobs to return a command on a channel
func main() {
	authDropbox()
	
	callback := make(chan *Chunk)
	go dropboxscrape(callback)
	// add new jobs here

	for {
		fmt.Printf("starting loop\n")
		
		chunk := <-callback
		switch chunk.Command {
		case "republish":
			go rebuildSite()
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
	fmt.Printf("rebuilding %#v\n",db.Creds)
	
	source := db.GetFileMeta("source")
	for _, textfile := range source.Contents {
		// fmt.Printf("text %v: %#v\n", i, text.Path)
		if strings.Index(textfile.Path, " ") != -1 {
			fmt.Printf("one of your files has a space in it! Fix it!\n")
		} else {
			text, err := db.GetFile(textfile.Path)
			if err != nil {
				fmt.Printf("error getting file text: %v",err)
			}
			fmt.Printf("got this file text: %v\n",text)
		}
	}
}

func authDropbox() {
	savedcreds, err := load("config.json")
	if err != nil {
		tempcred, err := db.Oauth.RequestTemporaryCredentials(http.DefaultClient, callback_url)
		if err != nil {
			fmt.Printf("err! %v", err)
			return
		}

		url := db.Oauth.AuthorizationURL(tempcred)
		fmt.Printf("auth url: %v\n", url)

		time.Sleep(15e9)

		newcreds, _, err := db.Oauth.RequestToken(http.DefaultClient, tempcred, "")
		err = save("config.json", newcreds.Token, newcreds.Secret)
		db.Creds = newcreds
	} else {
		fmt.Printf("loaded creds: %v\n", savedcreds)
		db.Creds = savedcreds

		// test for account info
		// fmt.Printf("results: %v\n", drop.AccountInfo(creds))

		// test for get file
		// drop.GetFile(creds,"LLTP5.jpg")

		// test for file meta
		// newfilemeta := drop.GetFileMeta(creds, "folder")
		// fmt.Printf("files: %#v\n", newfilemeta)
		// for i, thing := range newfilemeta.Contents {
		// 	fmt.Printf("file %v: %#v\n", i, thing)
		// }
	}
}

func save(fileName string, accessToken string, accessSecret string) error {
	config := oauth.Credentials{
		Token:  accessToken,
		Secret: accessSecret,
	}

	b, err := json.Marshal(config)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(fileName, b, 0600)
	if err != nil {
		return err
	}

	return nil
}

func load(fileName string) (*oauth.Credentials, error) {
	b, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	config := new(oauth.Credentials)

	err = json.Unmarshal(b, &config)
	if err != nil {
		return nil, err
	}
	return config, nil
}