# go-publisher

I've always disliked the dumb interfaces for blog tools that give me 99% more background color options than I want. I ended up always writing in something like Textmate or, more recently, iA Writer and then copy/pasting into some web interface and hitting a big save button. There was so much configuration tweaking to do between the part I enjoyed - writing - and publishing that I rarely made it through the whole process.

This publishing framework fixes these issues for me. I write markdown files that are saved to Dropbox so I can edit or create a post from almost any device. New posts are automatically detected, site files built and transferred to my server without me having to click any buttons or tweak any settings. Additionally, new links in Pinboard with a certain tag are pulled in and saved as posts at regular intervals. Just. Fucking. Write.

See the longer form post I wrote on this kind of thing [on my blog](http://blog.nickoneill.name/this-is-how-i-blog.html).

Also, go-publisher is a dumb name. It needs something clever.

### Foundations:

* you already write long-form with your favorite tools
* you already have streams of short-form stuff
* you don't want to be constrained to a single computer/device
* you don't want to babysit a publishing platform

### Setup

Go1 compatible.

Use `git clone http://github.com/nickoneill/go-publisher` in your `src` of GOPATH.

`cd go-publisher` and `cp config.json.ex config.json` before filling in some details in the config file, like your Dropbox key, secret and your Rsync details.

And finally `go run publisher.go` to get things started.