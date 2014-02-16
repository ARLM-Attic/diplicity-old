package web

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"code.google.com/p/go.net/websocket"
	"github.com/zond/diplicity/common"
	"github.com/zond/diplicity/game"
	"github.com/zond/diplicity/openid"
	"github.com/zond/diplicity/user"
	dip "github.com/zond/godip/common"
	"github.com/zond/kcwraps/subs"
)

var chatMessagesPattern = regexp.MustCompile("^/games/(.*)/messages$")
var gamePattern = regexp.MustCompile("^/games/(.*)$")

func (self *Web) WS(ws *websocket.Conn) {
	email := ""
	loggedIn := false
	if ws.Request().URL.Query().Get("openid.ext1.value.email") != "" {
		_, email, loggedIn = openid.VerifyAuth(ws.Request())
	} else {
		if token := ws.Request().URL.Query().Get("token"); self.env == "development" || token != "" {
			email = ws.Request().URL.Query().Get("email")
			if self.env == "development" {
				loggedIn = email != ""
			} else {
				timeout := common.MustParseInt64(ws.Request().URL.Query().Get("timeout"))
				if now := time.Now().UnixNano(); timeout < now {
					self.Errorf("\t%v\t%v\t[token too old: %v < %v]", ws.Request().URL, ws.Request().RemoteAddr, timeout, now)
					return
				}
				correct := common.NewTokenWithTimeout(self.secret, email, timeout)
				if correct.Token != token {
					self.Errorf("\t%v\t%v\t[bad token: %v != %v]", ws.Request().URL, ws.Request().RemoteAddr, token, correct)
					return
				}
				loggedIn = true
			}
		}
	}

	self.Infof("\t%v\t%v\t%v <-", ws.Request().URL, ws.Request().RemoteAddr, email)

	pack := subs.New(self.db, ws).OnUnsubscribe(func(s *subs.Subscription, reason interface{}) {
		self.Errorf("\t%v\t%v\t%v\t%v\t%v\t[unsubscribing]", ws.Request().URL.Path, ws.Request().RemoteAddr, email, s.URI(), reason)
		if self.logLevel > Trace {
			self.Tracef("%s", debug.Stack())
		}
	}).Logger(func(name string, i interface{}, op string, dur time.Duration) {
		self.Debugf("\t%v\t%v\t%v\t%v\t%v\t%v ->", ws.Request().URL.Path, ws.Request().RemoteAddr, email, op, name, dur)
	})
	defer func() {
		self.Infof("\t%v\t%v\t%v -> [unsubscribing all]", ws.Request().URL.Path, ws.Request().RemoteAddr, email)
		pack.UnsubscribeAll()
	}()

	var start time.Time
	for {
		var message subs.Message
		if err := websocket.JSON.Receive(ws, &message); err == nil {
			start = time.Now()
			if err = self.handleWSMessage(ws, email, loggedIn, pack, &message); err != nil {
				if message.Method != nil {
					self.Errorf("\t%v\t%v\t%v\t%v\t%v\t%v", ws.Request().URL.Path, ws.Request().RemoteAddr, email, message.Type, message.Method.Name, err)
				} else if message.Object != nil {
					self.Errorf("\t%v\t%v\t%v\t%v\t%v\t%v", ws.Request().URL.Path, ws.Request().RemoteAddr, email, message.Type, message.Object.URI, err)
				} else {
					self.Errorf("\t%v\t%v\t%v\t%+v\t%v", ws.Request().URL.Path, ws.Request().RemoteAddr, email, message, err)
				}
			}
			if message.Method != nil {
				self.Debugf("\t%v\t%v\t%v\t%v\t%v\t%v <-", ws.Request().URL.Path, ws.Request().RemoteAddr, email, message.Type, message.Method.Name, time.Now().Sub(start))
			}
			if message.Object != nil {
				self.Debugf("\t%v\t%v\t%v\t%v\t%v\t%v <-", ws.Request().URL.Path, ws.Request().RemoteAddr, email, message.Type, message.Object.URI, time.Now().Sub(start))
			}
			if self.logLevel > Trace {
				if message.Method != nil && message.Method.Data != nil {
					self.Tracef("%+v", common.Prettify(message.Method.Data))
				}
				if message.Object != nil && message.Object.Data != nil {
					self.Tracef("%+v", common.Prettify(message.Object.Data))
				}
			}
		} else if err == io.EOF {
			break
		} else {
			self.Errorf("%v", err)
		}
	}
}

func (self *Web) handleWSMessage(ws *websocket.Conn, email string, loggedIn bool, pack *subs.Pack, message *subs.Message) (err error) {
	authenticated := func() bool {
		if loggedIn {
			return true
		}
		self.Errorf("Unauthenticated access %+v", message)
		return false
	}
	unrecognized := func() error {
		self.Errorf("Unrecognized message %+v", common.Prettify(message))
		return nil
	}
	switch message.Type {
	case common.SubscribeType:
		s := pack.New(message.Object.URI)
		switch message.Object.URI {
		case "/games/current":
			if authenticated() {
				return game.SubscribeCurrent(self, s, email)
			}
		case "/games/open":
			if authenticated() {
				return game.SubscribeOpen(self, s, email)
			}
		case "/user":
			if loggedIn {
				return user.SubscribeEmail(self, s, email)
			} else {
				return s.Call(&user.User{}, subs.FetchType)
			}
		default:
			if match := chatMessagesPattern.FindStringSubmatch(message.Object.URI); match != nil {
				if authenticated() {
					return game.SubscribeMessages(self, s, match[1], email)
				}
			} else if match := gamePattern.FindStringSubmatch(message.Object.URI); match != nil {
				if authenticated() {
					return game.SubscribeGame(self, s, match[1], email)
				}
			} else {
				return unrecognized()
			}
		}
	case common.UnsubscribeType:
		pack.Unsubscribe(message.Object.URI)
		return
	case common.CreateType:
		switch message.Object.URI {
		case "/games":
			if authenticated() {
				return game.Create(self, subs.JSON{message.Object.Data}, email)
			}
		default:
			if match := chatMessagesPattern.FindStringSubmatch(message.Object.URI); match != nil {
				if authenticated() {
					return game.CreateMessage(self, subs.JSON{message.Object.Data}, email)
				}
			} else {
				return unrecognized()
			}
		}
	case common.DeleteType:
		if match := gamePattern.FindStringSubmatch(message.Object.URI); match != nil {
			if authenticated() {
				return game.DeleteMember(self, match[1], email)
			}
		} else {
			return unrecognized()
		}
	case common.UpdateType:
		if strings.Index(message.Object.URI, "/games/") == 0 {
			if authenticated() {
				return game.AddMember(self, subs.JSON{message.Object.Data}, email)
			} else if strings.Index(message.Object.URI, "/user") == 0 {
				if authenticated() {
					return user.Update(self.DB(), subs.JSON{message.Object.Data}, email)
				}
			} else {
				return unrecognized()
			}
		}
	case common.RPCType:
		switch message.Method.Name {
		case "GetPossibleSources":
			if authenticated() {
				params := subs.JSON{message.Method.Data}
				result := []dip.Province{}
				if result, err = game.GetPossibleSources(self, params.GetString("GameId"), email); err != nil {
					return
				}
				if err = websocket.JSON.Send(ws, subs.Message{
					Type: common.RPCType,
					Method: &subs.Method{
						Name: message.Method.Name,
						Id:   message.Method.Id,
						Data: result,
					},
				}); err != nil {
					return
				}
				return
			}
		case "GetValidOrders":
			if authenticated() {
				params := subs.JSON{message.Method.Data}
				options := dip.Options{}
				if options, err = game.GetValidOrders(self, params.GetString("GameId"), params.GetString("Province"), email); err != nil {
					return
				}
				if err = websocket.JSON.Send(ws, subs.Message{
					Type: common.RPCType,
					Method: &subs.Method{
						Name: message.Method.Name,
						Id:   message.Method.Id,
						Data: options,
					},
				}); err != nil {
					return
				}
				return
			}
		case "SetOrder":
			if authenticated() {
				params := subs.JSON{message.Method.Data}
				result := game.SetOrder(self, params.GetString("GameId"), params.GetStringSlice("Order"), email)
				data := ""
				if result != nil {
					data = result.Error()
				}
				if err = websocket.JSON.Send(ws, subs.Message{
					Type: common.RPCType,
					Method: &subs.Method{
						Name: message.Method.Name,
						Id:   message.Method.Id,
						Data: data,
					},
				}); err != nil {
					return
				}
				return
			}
		}
	}
	return unrecognized()
}

func (self *Web) Openid(w http.ResponseWriter, r *http.Request) {
	data := self.GetRequestData(w, r)
	redirect, email, ok := openid.VerifyAuth(r)
	if ok {
		data.session.Values[SessionEmail] = email
		user.EnsureUser(self.DB(), email)
	} else {
		delete(data.session.Values, SessionEmail)
	}
	data.Close()
	w.Header().Set("Location", redirect.String())
	w.WriteHeader(302)
	fmt.Fprintln(w, redirect.String())
}

func (self *Web) Token(w http.ResponseWriter, r *http.Request) {
	data := self.GetRequestData(w, r)
	if emailIf, found := data.session.Values[SessionEmail]; found {
		email := fmt.Sprint(emailIf)
		common.RenderJSON(w, common.NewToken(self.secret, email))
	} else {
		common.RenderJSON(w, common.Token{})
	}
}

func (self *Web) Logout(w http.ResponseWriter, r *http.Request) {
	data := self.GetRequestData(w, r)
	var redirect *url.URL
	r.ParseForm()
	if returnTo := r.Form.Get("return_to"); returnTo == "" {
		redirect = common.MustParseURL("http://" + r.Host + "/")
	} else {
		redirect = common.MustParseURL(returnTo)
	}
	delete(data.session.Values, SessionEmail)
	data.Close()
	w.Header().Set("Location", redirect.String())
	w.WriteHeader(302)
	fmt.Fprintln(w, redirect.String())
}

func (self *Web) Login(w http.ResponseWriter, r *http.Request) {
	var redirect *url.URL
	r.ParseForm()
	if returnTo := r.Form.Get("return_to"); returnTo == "" {
		redirect = common.MustParseURL("http://" + r.Host + "/")
	} else {
		redirect = common.MustParseURL(returnTo)
	}
	url := openid.GetAuthURL(r, redirect)
	w.Header().Set("Location", url.String())
	w.WriteHeader(302)
	fmt.Fprintln(w, url.String())
}

func (self *Web) Index(w http.ResponseWriter, r *http.Request) {
	data := self.GetRequestData(w, r)
	common.SetContentType(w, "text/html; charset=UTF-8", false)
	self.renderText(w, r, self.htmlTemplates, "index.html", data)
}

func (self *Web) AppCache(w http.ResponseWriter, r *http.Request) {
	if self.appcache {
		data := self.GetRequestData(w, r)
		w.Header().Set("Content-Type", "AddType text/cache-manifest .appcache; charset=UTF-8")
		self.renderText(w, r, self.textTemplates, "diplicity.appcache", data)
	} else {
		w.WriteHeader(404)
	}
}

func (self *Web) AllJs(w http.ResponseWriter, r *http.Request) {
	data := self.GetRequestData(w, r)
	common.SetContentType(w, "application/javascript; charset=UTF-8", true)
	self.renderText(w, r, self.jsTemplates, "jquery-2.0.3.min.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "jquery.hammer.min.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "underscore-min.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "backbone-min.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "bootstrap.min.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "bootstrap-multiselect.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "log.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "util.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "panzoom.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "cache.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "wsBackbone.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "baseView.js", data)
	fmt.Fprintln(w, ";")
	self.renderText(w, r, self.jsTemplates, "dippyMap.js", data)
	fmt.Fprintln(w, ";")
	self.render_Templates(data)
	fmt.Fprintln(w, ";")
	for _, templ := range self.jsModelTemplates.Templates() {
		if err := templ.Execute(w, data); err != nil {
			panic(err)
		}
		fmt.Fprintln(w, ";")
	}
	for _, templ := range self.jsCollectionTemplates.Templates() {
		if err := templ.Execute(w, data); err != nil {
			panic(err)
		}
		fmt.Fprintln(w, ";")
	}
	for _, templ := range self.jsViewTemplates.Templates() {
		if err := templ.Execute(w, data); err != nil {
			panic(err)
		}
		fmt.Fprintln(w, ";")
	}
	self.renderText(w, r, self.jsTemplates, "app.js", data)
	fmt.Fprintln(w, ";")
}

func (self *Web) AllCss(w http.ResponseWriter, r *http.Request) {
	data := self.GetRequestData(w, r)
	w.Header().Set("Cache-Control", "public, max-age=864000")
	w.Header().Set("Content-Type", "text/css; charset=UTF-8")
	self.renderText(w, r, self.cssTemplates, "bootstrap.min.css", data)
	self.renderText(w, r, self.cssTemplates, "bootstrap-theme.min.css", data)
	self.renderText(w, r, self.cssTemplates, "bootstrap-multiselect.css", data)
	self.renderText(w, r, self.cssTemplates, "slider.css", data)
	self.renderText(w, r, self.cssTemplates, "common.css", data)
}
