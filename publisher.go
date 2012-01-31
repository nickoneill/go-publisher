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
	"regexp"
	"github.com/garyburd/go-oauth"
	"github.com/nickoneill/go-dropbox"
	"launchpad.net/goyaml"
	"github.com/hoisie/mustache.go"
)

const app_key = "ylg2zoaj78ol2dz"
const app_secret = "i2863bf9odkbdl7"
const callback_url = "http://www.someurl.com/callback"

var (
	db = dropbox.NewClient(app_key, app_secret)
	lastbuild = time.Now()
	// creds = new(oauth.Credentials)
)

type Chunk struct {
	Command string
}

type Post struct {
	Published bool
	Title string
	Date string
	Content string
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

		source := db.GetFileMeta("source")
		
		needsrebuild := false
		for _, textfile := range source.Contents {
			// check each file for its modified date vs our last build date
			changed, err := time.Parse(time.RFC1123Z, textfile.Modified)
			if err != nil {
				fmt.Printf("error parsing modified date %v",err)
			}
			
			if lastbuild.Before(changed) {
				needsrebuild = true
			} else {
				// nothing
			}
		}
		
		// TODO: only rebuild newer posts? here? in rebuild?
		if needsrebuild {
			back <- &Chunk{Command: "republish"}
		}

		time.Sleep(20*time.Second)
	}
}

// basic rebuild command, builds site from dropbox files and deploys to configured location
func rebuildSite() {
	fmt.Printf("rebuilding\n")
	
	posttemplate, _ := db.GetFile("templates/post.mustache")
	hometemplate, _ := db.GetFile("templates/home.mustache")
	
	posts := make([]Post,1)
	
	source := db.GetFileMeta("source")
	for _, textfile := range source.Contents {
		// grab the contents of each source file
		text, err := db.GetFile(textfile.Path)
		if err != nil {
			fmt.Printf("error getting file text: %v\n",err)
		}

		// parse yaml front matter, or don't act
		if strings.HasPrefix(text, "---") {
			p := Post{}
			
			parts := strings.SplitN(text, "---\n", 3)
			
			goyaml.Unmarshal([]byte(parts[1]), &p)
			p.Content = parts[2]
			// TODO: check for partial yaml and fill it
			
			// publish individual posts, ignore drafts
			if p.Published {
				posts = append(posts, p)
				out := mustache.Render(posttemplate, map[string]string{"content": p.Content, "title": p.Title})
				
				pubpath := slugify(fmt.Sprintf("publish/%v.html",p.Title))
				db.PutFile(pubpath, out)
			} else {
				fmt.Printf("\"%v\" is marked as draft, not publishing",p.Title)
			}
		} else {
			fmt.Printf("file with path \"%v\" is not a registered doc\n",textfile.Path)
			// scrape should register new posts, not republish
		}
	}
	
	fmt.Printf("total posts: %v\n",len(posts))
	out := mustache.Render(hometemplate, map[string]interface{}{"posts": posts})
	
	db.PutFile("publish/index.html",out)
	fmt.Printf("Done publishing!\n")
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
		//fmt.Printf("loaded creds: %v\n", savedcreds)
		db.Creds = savedcreds
	}
}

func slugify(orig string) string {
	// removelist = [...]string{"a", "an", "as", "at", "before", "but", "by", "for","from","is", "in", "into", "like", "of", "off", "on", "onto","per","since", "than", "the", "this", "that", "to", "up", "via","with"}
	
	// for _, val := range removelist {
	// 	remover = regexp.MustCompile("\b"+val+"\b")
	// 	
	// }
	replaced := regexp.MustCompile("[\\s]").ReplaceAll([]byte(orig), []byte("_"))
	return string(replaced)
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