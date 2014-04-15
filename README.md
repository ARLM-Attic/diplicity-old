diplicity
=========

Next generation Droidippy.

## Forum and mailing list

http://groups.google.com/group/diplicity-dev

## Run locally

* Install Go: https://code.google.com/p/go/downloads/list
* Create a Go workspace: `mkdir $HOME/go`
* Set a GOPATH: `export GOPATH=$HOME/go`
* Prepare the workspace for the diplicity project: `mkdir $GOPATH/src/github.com/zond`
* Check out the project: `cd $GOPATH/src/github.com/zond && git clone git@github.com:zond/diplicity.git`
* Install the dependencies: `cd $GOPATH/src/github.com/zond/diplicity && go get -u -v ./...`
* Run the server locally and without appcache: `cd $GOPATH/src/github.com/zond/diplicity && go run diplicity/diplicity.go -appcache=false`

If you want to know other options when running locally: `cd $GOPATH/src/github.com/zond/diplicity && go run diplicity/diplicity.go -h`

## Fundamental ideas

* Mobile first
* Full offline mode for reading data
 * Likely no creating of data while offline, and automatic sync when offline, but it would be nice
* One interface for iPhone, Android and web

### Goals

* Most of the features of Droidippy
* Easier adding of new maps and variants
* Full support for primarily iOS and Android
  * Via mobile web pages
  * Via native web view wrappers with push notification support
* Full functionality in regular computer browsers
* Easier operations and hosting
* Simpler and more maintainable code
  * By rewriting from scratch
  * By using Go instead of Java
  * Yes, this has less of a developer community, but by god the code is simpler
* Shared burden of development, maintenance and support
* Self moderation of the games
  * By using some kind of voting system in the games to silence abusive players

### Non goals

* The best computer browser experience
* Exact duplication of Droidippy features

### Anti goals

* Separate code base for each platform

## Current design

* Backend implemented in Go
* Backend API 100% real time via WebSockets
* Frontend single page JavaScript application
* Frontend UI based on Bootstrap.js version 3
* Frontend framework built using Backbone.js routes, views and models

