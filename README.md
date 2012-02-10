# go-publisher

I've always disliked the dumb interfaces for blog tools that give me 99% more background color options than I want. I ended up always writing in something like Textmate or, more recently, iA Writer and then copy/pasting into some web interface and hitting a big save button. There was so much configuration tweaking to do between the part I enjoyed - writing - and publishing that I rarely made it through the whole process.

This publishing framework fixes these issues for me. I write markdown files that are saved to Dropbox so I can edit or create a post from almost any device. New posts are automatically detected, site files built and transferred to my server without me having to click any buttons or tweak any settings. Additionally, new links in Pinboard with a certain tag are pulled in and saved as posts at regular intervals. Just. Fucking. Write.

Also, go-publisher is a dumb name. It needs something clever.

### Foundations:

* you already write long-form with your favorite tools
* you already have streams of short-form stuff
* you don't want to be constrained to a single computer/device
* you don't want to babysit a publishing platform

### Setup

Not compatible with the latest weekly yet (I haven't figured out how to properly use GOPATH yet). It should work with any of the weekly releases from some time in November until 2012-01-20.

Install the 

`goinstall github.com/garyburd/go-oauth`
`goinstall github.com/nickoneill/go-dropbox`
`goinstall launchpad.net/goyaml`

`git clone https://github.com/hoisie/mustache.go.git`
apply patch at https://github.com/jeffbr13/mustache.go/commit/33acde5032d6c4c7f33cb80ee4812559b1a9f2a0
`gomake install`

`git clone https://github.com/russross/blackfriday.git`
apply patch at https://github.com/jteeuwen/blackfriday/commit/ec0ed69226d5280b2a41d8a4990acccfb4360ce5
`gomake install`