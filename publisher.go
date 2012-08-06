package main

import (
	"fmt"
	"time"
	"io/ioutil"
	"net/http"
	"html"
	"encoding/json"
	"encoding/xml"
	"strings"
	"sort"
	"io"
	"os"
	"os/exec"
	"regexp"
	"github.com/garyburd/go-oauth/oauth"
	"github.com/nickoneill/go-dropbox"
	"launchpad.net/goyaml"
	"github.com/drhodes/mustache.go"
	"github.com/russross/blackfriday"
)

var _ = os.Stdout

var (
	config Config
	db *dropbox.DropboxClient
)

type Chunk struct {
	Command string
}

type Config struct {
	DropboxKey string
	DropboxSecret string
	OauthCredentials *oauth.Credentials
	LastBuildTime string
	PinboardFeedURL string
	LastPinboardCheck string
	Rsync *RsyncOptions
	Debug bool
	Publish bool
}

type RsyncOptions struct {
	Domain string
	Username string
	RemoteDir string
}

type RDF struct {
	Items []PinboardItem `xml:"item"`
}

type PinboardItem struct {
	Title string `xml:"title"`
	Link string `xml:"link"`
	Date string `xml:"date"`
	Sourcedate string
	Description string `xml:"description"`
}

type Post struct {
	Published bool
	Title string
	Date string
	NiceDate string
	RFC3339Date string
	Content string
	Excerpt string
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
		fmt.Printf("error parsing date for i: %v\n",err)
	}
	jdate, err := time.Parse("2006-01-02 15:04", p.Posts[j].Date)
	if err != nil {
		fmt.Printf("error parsing date for j: %v\n",err)
	}
	
	return idate.After(jdate)
}

func (p PostContainer) Swap(i, j int) {
	p.Posts[i], p.Posts[j] = p.Posts[j], p.Posts[i]
}

// main just loops and waits for jobs to return a command on a channel
func main() {
	load("config.json")
	
	if config.DropboxKey == "PUTYERKEYHERE" || config.DropboxKey == "" {
		fmt.Println("You need to add your dropbox key to config")
	} else {
		db = dropbox.NewClient(config.DropboxKey, config.DropboxSecret)
		db.Token = "NO"
		
		if config.OauthCredentials.Token != "" {
			db.Creds = config.OauthCredentials
		} else {
			authDropbox()
			db.Creds = config.OauthCredentials
		}

		callback := make(chan *Chunk)

		// add new document creation jobs here
		if (config.PinboardFeedURL != "") {
			go pinboardscape()
		}

		go registrar(callback)

		for {
			chunk := <-callback
			switch chunk.Command {
			case "republish":
				go rebuildSite()
			}
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
		if config.Debug {
			time.Sleep(20*time.Second)
		} else {
			time.Sleep(5*time.Minute)
		}
		
		if config.OauthCredentials.Token != "" {
			source := db.GetFileMeta("source")

			needsrebuild := false
			for _, textfile := range source.Contents {
				//fmt.Printf("item in folder %v\n",textfile.Path)
				// check each file for its modified date vs our last build date
				changed, err := time.Parse(time.RFC1123Z, textfile.Modified)
				if err != nil {
					fmt.Printf("error parsing modified date %v",err)
				}
				
				text, err := db.GetFile(textfile.Path)
				if err != nil {
					fmt.Printf("error getting file text: %v\n",err)
				}
				
				if strings.HasPrefix(text, "---") {
					p := Post{}
					parts := strings.SplitN(text, "---\n", 3)
					goyaml.Unmarshal([]byte(parts[1]), &p)
					
					if p.Published {
						if p.Date == "" {
							fmt.Println("fill in the date on this published post")
						}
						if p.Title == "" {
							fmt.Println("fill in the title on this published post")
						}
						
						lastbuild, _ := time.Parse(time.RFC3339, config.LastBuildTime)
						if lastbuild.Before(changed) {
							// TODO: check if the file is published before we decide we should republish
							fmt.Println("dropbox source needs rebuild")
							needsrebuild = true
						}
					}
				}
			}

			// TODO: only rebuild newer posts? here? in rebuild?
			if needsrebuild {
				back <- &Chunk{Command: "republish"}
			} else {
				fmt.Println("no changes to be rebuilt")
			}
		} else {
			fmt.Println("no dropbox creds, not checking")
		}
	}
}

// this scrape manages an external pinboard feed
// it should:
// * pull content from new items
// * generate published source documents
func pinboardscape() {
	for {
		fmt.Printf("fetching pinboard feed\n")
		res, err := http.Get(config.PinboardFeedURL)
		if err != nil {
			fmt.Printf("error getting feed: %v\n",err)
		}
		
		newlast := time.Time{}
		if res != nil {
			defer res.Body.Close()
			body, err := ioutil.ReadAll(res.Body)
			if err != nil {
				fmt.Println("Couldn't read response: %v",err)
			}

			//content, err := ioutil.ReadAll(res.Body)
			//fmt.Printf("body: %v",string(content))

			feed := RDF{}
			err = xml.Unmarshal(body, &feed)
			if err != nil {
				fmt.Printf("feed error: %v\n",err)
			}
			
			sourcetemplate, err := ioutil.ReadFile("templates/source.mustache")
			if err != nil {
				fmt.Printf("error getting source template: %v\n",err)
			}
			
			lasttime, _ := time.Parse(time.RFC3339, config.LastPinboardCheck)
			for _, item := range feed.Items {
				itemdate, err := time.Parse(time.RFC3339, item.Date)
				//fmt.Printf("itemdate: %v\n",itemdate.Format(time.RFC3339))
				if err != nil {
					fmt.Printf("error parsing item date: %v\n",err)
				}
				item.Sourcedate = itemdate.Format("2006-01-02 15:04")
				//fmt.Printf("sourcedate: %v\n",item.Sourcedate)
				
				// need to keep track of newest pinboard post, don't want to assume it's first
				if newlast.Before(itemdate) {
					newlast = itemdate
				}
				
				// fmt.Printf("compare item: %v to last: %v\n",itemdate,lasttime)
				if lasttime.Before(itemdate) {
					fmt.Printf("new pinboard post with name: %v\n",item.Title)
					filename := slugify(item.Title)
					
					out := mustache.Render(string(sourcetemplate), map[string]interface{}{"post": &item})
					// TODO: check the path for existing file, raise a warning if a file exists at this path already
					db.PutFile("source/"+filename+".md", out)
				}
			}
		}
		
		// pinboard time is subtly different, we store time of newest item processed
		config.LastPinboardCheck = newlast.Format(time.RFC3339)
		_ = save("config.json")
		
		if config.Debug {
			time.Sleep(25*time.Second)
		} else {
			time.Sleep(4*time.Minute)
		}
	}
}

// basic rebuild command, builds site from dropbox files and deploys to configured location
func rebuildSite() {
	fmt.Printf("rebuilding\n")
	
	posttemplate, _ := db.GetFile("templates/post.mustache")
	hometemplate, _ := db.GetFile("templates/home.mustache")
	feedtemplate, _ := db.GetFile("templates/feed.mustache")
	archivetemplate, _ := db.GetFile("templates/archive.mustache")
	
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
			
			// fmt.Printf("%v: %v",textfile.Path,[]byte(text[0:5]))
			parts := strings.SplitN(text, "---\n", 3)
			
			goyaml.Unmarshal([]byte(parts[1]), &p)
			p.Content = string(blackfriday.MarkdownCommon([]byte(parts[2])))
			if len(parts[2]) > 200 { // trim for excerpt
				p.Excerpt = html.EscapeString(parts[2][0:200])
			} else {
				p.Excerpt = parts[2]
			}
			p.Filename = slugify(p.Title)+".html"
			// TODO: check for partial yaml and fill it
			
			// publish individual posts, ignore drafts
			if p.Published {
				date, err := time.Parse("2006-01-02 15:04", p.Date)
				if err != nil {
					fmt.Printf("Can't parse date for this post: %v\n",p.Date)
				}
				p.RFC3339Date = date.Format(time.RFC3339)
				p.NiceDate = date.Format("January 2, 2006")
				p.Atomid = generateAtomId(p)
				pc.Posts = append(pc.Posts, p)
				
				out := mustache.Render(posttemplate, map[string]interface{}{"post": &p})
				
				err = ioutil.WriteFile(tmppath+"/"+p.Filename, []byte(out), 0644)
				if err != nil {
					fmt.Printf("error writing file: %v\n",err)
				}
				//db.PutFile(pubpath, out)
			} else {
				fmt.Printf("\"%v\" is marked as draft, not publishing\n",p.Title)
			}
		} else {
			fmt.Printf("file with path \"%v\" is not a registered doc\n",textfile.Path)
			// scrape should register new posts, not republish
		}
	}
	
	fmt.Printf("total posts: %v\n",len(pc.Posts))
	sort.Sort(pc)
	
	// build the home file
	homeposts := []Post{}
	if len(pc.Posts) < 12 {
		homeposts = pc.Posts[:]
	} else {
		homeposts = pc.Posts[:12]
	}
	
	home := mustache.Render(hometemplate, map[string]interface{}{"posts": homeposts})
	
	ioutil.WriteFile(tmppath+"/index.html", []byte(home), 0644)
	//db.PutFile("publish/index.html",out)
	
	// build the feed file
	feedposts := []Post{}
	if len(pc.Posts) < 12 {
		feedposts = pc.Posts[:]
	} else {
		feedposts = pc.Posts[:12]
	}
	
	feed := mustache.Render(feedtemplate, map[string]interface{}{"posts": feedposts, "updated": time.Now().Format(time.RFC3339)})
	ioutil.WriteFile(tmppath+"/atom.xml", []byte(feed), 0644)

	// build the archive file
	archiveposts := []Post{}
	archiveposts = pc.Posts[:]
	
	archive := mustache.Render(archivetemplate, map[string]interface{}{"posts": archiveposts})
	ioutil.WriteFile(tmppath+"/archive.html", []byte(archive), 0644)
	
	fmt.Printf("Done site generation at %v\n",tmppath)
	
	if config.Publish {
		// copy files in resources to tmp
		resources := db.GetFileMeta("resources")
		for _, textfile := range resources.Contents {
			source, err := db.GetFile(textfile.Path)
			if err != nil {
				fmt.Printf("error getting resource: %v\n",err)
			}

			filename := strings.Replace(textfile.Path, "/resources/", "", 1)
			ioutil.WriteFile(tmppath+"/"+filename, []byte(source), 0644)
		}

		// rsync to specified server
		rsync(tmppath+"/", config.Rsync.Username, config.Rsync.Domain, config.Rsync.RemoteDir)

		// save last build time
		config.LastBuildTime = time.Now().Format(time.RFC3339)
		_ = save("config.json")
	}
}

func authDropbox() {
	tempcred, err := db.Oauth.RequestTemporaryCredentials(http.DefaultClient, "", nil)
	if err != nil {
		fmt.Printf("err! %v", err)
		return
	}

	url := db.Oauth.AuthorizationURL(tempcred, nil)
	fmt.Printf("you have 20 seconds to visit this auth url in your browser: %v\n", url)

	time.Sleep(20*time.Second)
	fmt.Println("requesting permanent credentials")

	newcreds, _, err := db.Oauth.RequestToken(http.DefaultClient, tempcred, "")
	config.OauthCredentials = newcreds
	db.Creds = newcreds
	_ = save("config.json")
}

func generateAtomId(p Post) string {
	pre := fmt.Sprintf("tag:%s,",config.Rsync.Domain)
	date, _ := time.Parse(time.RFC3339, p.RFC3339Date)
	formatdate := date.Format("2006-01-15")
	perm := ":/"+p.Filename
	
	return pre+formatdate+perm
}

func slugify(orig string) string {
	slug := []byte(orig)
	// removelist = [...]string{"a", "an", "as", "at", "before", "but", "by", "for","from","is", "in", "into", "like", "of", "off", "on", "onto","per","since", "than", "the", "this", "that", "to", "up", "via","with"}
	
	// TODO: remove wordlist
	// replace spaces
	slug = regexp.MustCompile("[\\s]").ReplaceAll(slug, []byte("_"))
	// replace invalid characters
	slug = regexp.MustCompile("\\W").ReplaceAll(slug, []byte(""))
	// I like dashes
	slug = regexp.MustCompile("[_]").ReplaceAll(slug, []byte("-"))
	// lowercase
	return strings.ToLower(string(slug))
}

func rsync(source string, user string, host string, dest string) {
	fmt.Printf("rsync -rzv "+source+" "+user+"@"+host+":"+dest+"\n")
	
	cmd := exec.Command("rsync", "-rzv", source, user + "@" + host + ":" + dest)
	stdout, err := cmd.StderrPipe()
	go io.Copy(os.Stdout, stdout)

	err = cmd.Run()
	if err != nil {
		fmt.Printf("rsync error %v\n", err)
	}
	fmt.Println("rsync done")
}

func save(fileName string) error {
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

func load(fileName string) error {
	b, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, &config)
	if err != nil {
		return err
	}
	
	return nil
}