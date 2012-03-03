package main

import (
	"fmt"
	"time"
	"io/ioutil"
	"net/http"
	"encoding/json"
	"encoding/xml"
	"strings"
	"bytes"
	// "path/filepath"
	"sort"
	"io"
	"os"
	"os/exec"
	"regexp"
	"github.com/garyburd/go-oauth"
	"github.com/nickoneill/go-dropbox"
	"launchpad.net/goyaml"
	"html/template"
	// "github.com/hoisie/mustache.go"
	"github.com/russross/blackfriday"
)

var _ = os.Stdout

var (
	config Config
	db *dropbox.DropboxClient
	//lastbuild = time.Now().Add(-2*time.Hour)
)

type Chunk struct {
	Command string
}

type Config struct {
	DropboxKey string
	DropboxSecret string
	OauthCredentials *oauth.Credentials
	LastBuildTime string
	LastPinboardCheck string
	Debug bool
}

type RDF struct {
	//XMLName xml.Name `xml:"rdf"`
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
}

// this scrape manages the source directory
// it should:
// * automatically adding yaml front matter to things that don't have any (drafts)
// * fill in empty data for posts have have some data
// * issue rebuild requests for newly published documents
func registrar(back chan *Chunk) {
	for {
		if config.Debug {
			time.Sleep(10*time.Second)
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
				config.LastBuildTime = time.Now().Format(time.RFC3339)
				_ = save("config.json")
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
		res, err := http.Get("http://feeds.pinboard.in/rss/secret:861dae43105f37e6b08c/u:nickoneill/t:nickoneillblog/")
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
			
			sourcetemplate, err := template.ParseFiles("templates/source.mustache")//ioutil.ReadFile("templates/source.mustache")
			if err != nil {
				fmt.Printf("error getting source template: %v\n",err)
			}
			// fmt.Printf()
			
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
					
					var buf bytes.Buffer
					_ = sourcetemplate.Execute(&buf, map[string]interface{}{"post": &item})
					// out := mustache.Render(string(sourcetemplate), map[string]interface{}{"post": &item})
					db.PutFile("source/"+filename+".md", buf.String())
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
	// http://feeds.pinboard.in/rss/secret:861dae43105f37e6b08c/u:nickoneill/t:apple/
}

// basic rebuild command, builds site from dropbox files and deploys to configured location
func rebuildSite() {
	fmt.Printf("rebuilding\n")
	
	posttemplatestring, _ := db.GetFile("templates/post.mustache")
	posttemplate, err := template.New("post").Parse(posttemplatestring)
	hometemplatestring, _ := db.GetFile("templates/home.mustache")
	hometemplate, err := template.New("home").Parse(hometemplatestring)
	feedtemplatestring, _ := db.GetFile("templates/feed.mustache")
	feedtemplate, err := template.New("feed").Parse(feedtemplatestring)
	
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
				date, err := time.Parse("2006-01-02 15:04", p.Date)
				if err != nil {
					fmt.Printf("Can't parse date for this post: %v\n",p.Date)
				}
				p.RFC3339Date = date.Format(time.RFC3339)
				p.Atomid = generateAtomId(p)
				pc.Posts = append(pc.Posts, p)
				
				var buf bytes.Buffer
				_ = posttemplate.Execute(&buf, map[string]interface{}{"post": &p})
				// out := mustache.Render(posttemplate, map[string]interface{}{"post": &p})
				
				err = ioutil.WriteFile(tmppath+"/"+p.Filename, buf.Bytes(), 0644)
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
	if len(pc.Posts) < 10 {
		homeposts = pc.Posts[:]
	} else {
		homeposts = pc.Posts[:10]
	}
	
	var homebuf bytes.Buffer
	_ = hometemplate.Execute(&homebuf, map[string]interface{}{"posts": homeposts})
	// home := mustache.Render(hometemplate, map[string]interface{}{"posts": homeposts})
	
	ioutil.WriteFile(tmppath+"/index.html", homebuf.Bytes(), 0644)
	//db.PutFile("publish/index.html",out)
	
	// build the feed file
	feedposts := []Post{}
	if len(pc.Posts) < 10 {
		feedposts = pc.Posts[:]
	} else {
		feedposts = pc.Posts[:10]
	}
	
	var feedbuf bytes.Buffer
	_ = feedtemplate.Execute(&feedbuf, map[string]interface{}{"posts": feedposts, "updated": time.Now().Format(time.RFC3339)})
	// feed := mustache.Render(feedtemplate, map[string]interface{}{"posts": feedposts, "updated": time.Now().Format(time.RFC3339)})
	ioutil.WriteFile(tmppath+"/atom.xml", feedbuf.Bytes(), 0644)
	
	fmt.Printf("Done site generation at %v\n",tmppath)
	
	if !config.Debug {
		rsync(tmppath+"/*", "nickoneill", "nickoneill.name", "/var/www/blog.nickoneill.name/public_html/")
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
	pre := "tag:blog.nickoneill.name,"
	date, _ := time.Parse(time.RFC3339, p.RFC3339Date)
	formatdate := date.Format("2006-01-15")
	perm := ":/"+p.Filename
	
	return pre+formatdate+perm
}

func slugify(orig string) string {
	// removelist = [...]string{"a", "an", "as", "at", "before", "but", "by", "for","from","is", "in", "into", "like", "of", "off", "on", "onto","per","since", "than", "the", "this", "that", "to", "up", "via","with"}
	
	// TODO: remove wordlist
	// replace spaces
	sansspaces := regexp.MustCompile("[\\s]").ReplaceAll([]byte(orig), []byte("-"))
	// replace invalid characters
	noinvalid := regexp.MustCompile("\\W").ReplaceAll(sansspaces, []byte(""))
	// lowercase
	lowercase := strings.ToLower(string(noinvalid))
	return lowercase
}

func rsync(source string, user string, host string, dest string) {
	fmt.Printf("rsync -azv "+source+" "+user+"@"+host+":"+dest+"\n")
	
	cmd := exec.Command("rsync", "-azv", source, user + "@" + host + ":" + dest)
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