package main

import (
	"fmt"
	"time"
	"io/ioutil"
	"net/http"
	"encoding/json"
	"encoding/xml"
	"strings"
	// "path/filepath"
	"sort"
	"io"
	"os"
	"os/exec"
	"regexp"
	"github.com/garyburd/go-oauth"
	"github.com/nickoneill/go-dropbox"
	"launchpad.net/goyaml"
	"github.com/hoisie/mustache.go"
	"github.com/russross/blackfriday"
)

var _ = os.Stdout

const app_key = "ylg2zoaj78ol2dz"
const app_secret = "i2863bf9odkbdl7"
const callback_url = "http://www.someurl.com/callback"

var (
	db = dropbox.NewClient(app_key, app_secret)
	lastbuild = time.Now().Add(-2*time.Hour)
)

type Chunk struct {
	Command string
}

type RDF struct {
	//XMLName xml.Name `xml:"rdf"`
	Items []PinboardItem `xml:"item"`
}

type PinboardItem struct {
	Title string `xml:"title"`
	Link string `xml:"link"`
	// Author string `xml:"creator"`
	Date string `xml:"date"`
	Description string `xml:"description"`
}

type Post struct {
	Published bool
	Title string
	Date string
	RFC3339Date string
	Content string
	Filename string
	Atomid string
}

type PostContainer struct {
	Posts []Post
}

func (p PostContainer) Len() int {
	return len(p.Posts)
}

func (p PostContainer) Less(i, j int) bool {
	idate, err := time.Parse("2006-01-02 15:04", p.Posts[i].Date)
	if err != nil {
		fmt.Printf("error parsing date\n")
	}
	jdate, err := time.Parse("2006-01-02 15:04", p.Posts[j].Date)
	
	return idate.After(jdate)
}

func (p PostContainer) Swap(i, j int) {
	p.Posts[i], p.Posts[j] = p.Posts[j], p.Posts[i]
}

// main just loops and waits for jobs to return a command on a channel
func main() {
	authDropbox()
	
	callback := make(chan *Chunk)
	
	// add new document creation jobs here
	go pinboardscape()
	
	go registrar(callback)

	for {
		chunk := <-callback
		switch chunk.Command {
		case "republish":
			go rebuildSite()
		}
	}
}

// this scrape manages the source directory
// it should:
// * automatically adding yaml front matter to things that don't have any (drafts)
// * fill in empty data for posts have have some data
// * issue rebuild requests for newly published documents
func registrar(back chan *Chunk) {
	for {
		source := db.GetFileMeta("source")
		
		needsrebuild := false
		for _, textfile := range source.Contents {
			// check each file for its modified date vs our last build date
			changed, err := time.Parse(time.RFC1123Z, textfile.Modified)
			if err != nil {
				fmt.Printf("error parsing modified date %v",err)
			}
			
			if lastbuild.Before(changed) {
				fmt.Println("dropbox source needs rebuild")
				needsrebuild = true
			} else {
				// nothing
			}
		}
		
		// TODO: only rebuild newer posts? here? in rebuild?
		if needsrebuild {
			back <- &Chunk{Command: "republish"}
		} else {
			fmt.Println("no changes in dropbox source")
		}

		time.Sleep(20*time.Second)
	}
}

// this scrape manages an external pinboard feed
// it should:
// * pull content from new items
// * generate published source documents
func pinboardscape() {
	for {
		res, err := http.Get("http://feeds.pinboard.in/rss/secret:861dae43105f37e6b08c/u:nickoneill/t:apple/")
		if err != nil {
			fmt.Printf("error getting feed: %v\n",err)
		}
		
		if res != nil {
			defer res.Body.Close()

			//content, err := ioutil.ReadAll(res.Body)
			//fmt.Printf("body: %v",string(content))

			feed := RDF{}
			err = xml.Unmarshal(res.Body, &feed)
			if err != nil {
				fmt.Printf("feed error: %v\n",err)
			}

			fmt.Printf("feed: %v",feed)
		}
		
		time.Sleep(20*time.Second)
	}
	// http://feeds.pinboard.in/rss/secret:861dae43105f37e6b08c/u:nickoneill/t:apple/
}

// basic rebuild command, builds site from dropbox files and deploys to configured location
func rebuildSite() {
	fmt.Printf("rebuilding\n")
	
	posttemplate, _ := db.GetFile("templates/post.mustache")
	hometemplate, _ := db.GetFile("templates/home.mustache")
	feedtemplate, _ := db.GetFile("templates/feed.mustache")
	tmppath, err := ioutil.TempDir("","gopub")
	if err != nil {
		fmt.Printf("error creating tmp dir: %v",err)
	}
	
	pc := PostContainer{}
	
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
			p.Content = string(blackfriday.MarkdownCommon([]byte(parts[2])))
			p.Filename = slugify(p.Title)+".html"
			// TODO: check for partial yaml and fill it
			
			// publish individual posts, ignore drafts
			if p.Published {
				date, _ := time.Parse("2006-01-02 15:04", p.Date)
				p.RFC3339Date = date.Format(time.RFC3339)
				p.Atomid = generateAtomId(p)
				pc.Posts = append(pc.Posts, p)
				out := mustache.Render(posttemplate, map[string]interface{}{"post": &p})
				
				ioutil.WriteFile(tmppath+"/"+p.Filename, []byte(out), 0644)
				//db.PutFile(pubpath, out)
			} else {
				fmt.Printf("\"%v\" is marked as draft, not publishing\n",p.Title)
			}
		} else {
			fmt.Printf("file with path \"%v\" is not a registered doc\n",textfile.Path)
			// scrape should register new posts, not republish
		}
	}
	
	// build the home file
	fmt.Printf("total posts: %v\n",len(pc.Posts))
	sort.Sort(pc)
	home := mustache.Render(hometemplate, map[string]interface{}{"posts": pc.Posts[0:4]})
	
	ioutil.WriteFile(tmppath+"/index.html", []byte(home), 0644)
	//db.PutFile("publish/index.html",out)
	
	// build the feed file
	feed := mustache.Render(feedtemplate, map[string]interface{}{"posts": pc.Posts[0:10], "updated": time.Now().Format(time.RFC3339)})
	ioutil.WriteFile(tmppath+"/atom.xml", []byte(feed), 0644)
	
	fmt.Printf("Done site generation!\n")
	
	rsync(tmppath+"/", "nickoneill", "nickoneill.name", "/var/www/blog.nickoneill.name/public_html/test")
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

func generateAtomId(p Post) string {
	pre := "tag:blog.nickoneill.name,"
	date, _ := time.Parse(time.RFC3339, p.RFC3339Date)
	formatdate := date.Format("2006-01-15")
	perm := ":/"+p.Filename
	
	return pre+formatdate+perm
}

func slugify(orig string) string {
	// removelist = [...]string{"a", "an", "as", "at", "before", "but", "by", "for","from","is", "in", "into", "like", "of", "off", "on", "onto","per","since", "than", "the", "this", "that", "to", "up", "via","with"}
	
	// remove wordlist
	// replace spaces
	sansspaces := regexp.MustCompile("[\\s]").ReplaceAll([]byte(orig), []byte("-"))
	// lowercase
	lowercase := strings.ToLower(string(sansspaces))
	return lowercase
}

func rsync(source string, user string, host string, dest string) {
	fmt.Printf("rsync -razv "+source+" "+user+"@"+host+":"+dest+"\n")
	
	cmd := exec.Command("rsync", "-razv", "--chmod=u=rwX,go=rX", source, user + "@" + host + ":" + dest)
	stdout, err := cmd.StderrPipe()
	go io.Copy(os.Stdout, stdout)

	err = cmd.Run()
	if err != nil {
		fmt.Printf("rsync error %v\n", err)
	}
	fmt.Printf("rsync done")
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