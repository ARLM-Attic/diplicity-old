package common

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/jhillyerd/go.enmime"
	"github.com/zond/diplicity/game/allocation"
	"github.com/zond/diplicity/game/meta"
	"github.com/zond/gmail"
	"github.com/zond/godip/variants"
	"github.com/zond/unbolted"
	"github.com/zond/wsubs/gosubs"
)

const (
	SessionEmail = "email"
	SessionName  = "diplicity_session"
	Admin        = "Admin"
	Development  = "development"
)

const (
	Fatal = iota
	Error
	Info
	Debug
	Trace
)

type Server struct {
	sessionStore          *sessions.CookieStore
	db                    *unbolted.DB
	gmail                 *gmail.Client
	env                   string
	logLevel              int
	svgTemplates          *template.Template
	jsModelTemplates      *template.Template
	jsCollectionTemplates *template.Template
	jsTemplates           *template.Template
	cssTemplates          *template.Template
	_Templates            *template.Template
	jsViewTemplates       *template.Template
	gmailAccount          string
	gmailPassword         string
	smtpAccount           string
	smtpHost              string
	mailHandler           func(c SkinnyContext, msg *enmime.MIMEBody) error
	router                *Router
	secret                string
}

func NewServer(secret, env, db string) (self *Server, err error) {
	self = &Server{
		secret:       secret,
		env:          env,
		sessionStore: sessions.NewCookieStore([]byte(secret)),
	}
	if self.db, err = unbolted.NewDB(db); err != nil {
		return
	}
	self.router = newRouter(self)
	self.router.Secret = secret
	if env != Development {
		if secret == DefaultSecret {
			err = fmt.Errorf("Only development env can run with the default secret")
			return
		}
		self.logLevel = Trace
	} else {
		self.logLevel = Debug
		self.router.DevMode = true
	}
	self.router.LogLevel = self.logLevel
	return
}

var temps = template.Must(template.ParseGlob("templates/*"))

func (self *Server) Index(c *HTTPContext) (err error) {
	c.SetContentType("text/html; charset=UTF-8")
	f, err := os.Open(filepath.Join("static", "index.html"))
	if err != nil {
		return
	}
	defer f.Close()
	if _, err = io.Copy(c.Resp(), f); err != nil {
		return
	}
	return
}

func (self *Server) Go2JS(c *HTTPContext) (err error) {
	c.SetContentType("application/javascript; charset=UTF-8")
	if err = temps.ExecuteTemplate(c.Resp(), "go.js", self); err != nil {
		return
	}
	return
}
func (self *Server) Router() *Router {
	return self.router
}

func (self *Server) Env() string {
	return self.env
}

func (self *Server) IsSubscribing(principal, uri string, timeout time.Duration) bool {
	return self.router.IsSubscribing(principal, uri, timeout)
}

func (self *Server) Secret() string {
	return self.secret
}

func (self *Server) SendAddress() string {
	return self.smtpAccount
}

func (self *Server) ReceiveAddress() string {
	return self.gmailAccount
}

func (self *Server) SetSMTP(host, account string) *Server {
	self.smtpAccount = account
	self.smtpHost = host
	return self
}

func (self *Server) Start() (err error) {
	if self.gmailAccount != "" {
		self.gmail = gmail.New(self.gmailAccount, self.gmailPassword).MailHandler(self.IncomingMail).ErrorHandler(func(e error) {
			self.Fatalf("Mail handler: %v", e)
		})
		if _, err = self.gmail.Start(); err != nil {
			return
		}
		self.Infof("Listening to incoming mail from %#v", self.gmailAccount)
	}
	return
}

func (self *Server) IncomingMail(msg *enmime.MIMEBody) error {
	return self.mailHandler(self.Diet(), msg)
}

func (self *Server) Diet() SkinnyContext {
	return &skinnyServer{
		Server: self,
	}
}

func (self *Server) SendMail(fromName, replyTo, subject, message string, recips []string) (err error) {
	body := strings.Join([]string{
		"Content-Type: text/plain; charset=\"utf-8\"",
		fmt.Sprintf("Reply-To: %v", replyTo),
		fmt.Sprintf("From: %v <%v>", fromName, self.smtpAccount),
		fmt.Sprintf("To: %v", strings.Join(recips, ", ")),
		fmt.Sprintf("Subject: %v", subject),
		"",
		message,
	}, "\r\n")
	actualRecips := []string{}
	for _, recip := range recips {
		if match := gmail.AddrReg.FindString(recip); match != "" {
			actualRecips = append(actualRecips, match)
		}
	}
	if self.Env() == Development {
		self.Infof("Would have sent\n%v", body)
	} else {
		key := uint64(time.Now().UnixNano()<<32) + uint64(rand.Uint32())
		self.Infof("%v: Will try to send\n%v", key, body)
		if err = smtp.SendMail(self.smtpHost, nil, self.smtpAccount, actualRecips, []byte(body)); err != nil {
			self.Errorf("%v: Unable to send: %v", key, err)
			return
		}
		self.Infof("%v: Successfully sent", key)
	}
	return
}

func (self *Server) DB() *unbolted.DB {
	return self.db
}

func (self *Server) GetContext(w http.ResponseWriter, r *http.Request) (result *HTTPContext) {
	result = &HTTPContext{
		response: w,
		request:  r,
		web:      self,
		vars:     mux.Vars(r),
	}
	result.session, _ = self.sessionStore.Get(r, SessionName)
	return
}

func (self *Server) SetGMail(account, password string, handler func(c SkinnyContext, msg *enmime.MIMEBody) error) *Server {
	self.gmailAccount, self.gmailPassword, self.mailHandler = account, password, handler
	return self
}

func (self *Server) DevHandle(r *mux.Route, f func(c *HTTPContext) error) {
	if self.Env() == Development {
		self.Handle(r, func(c *HTTPContext) (err error) {
			if c.Env() == Development {
				return f(c)
			}
			c.Resp().WriteHeader(403)
			return
		})
	}
}

func (self *Server) AdminHandle(r *mux.Route, f func(c *HTTPContext) error) {
	self.Handle(r, func(c *HTTPContext) (err error) {
		tokenStr := c.Req().FormValue("token")
		if tokenStr == "" {
			err = fmt.Errorf("Missing token")
			return
		}
		token, err := gosubs.DecodeToken(self.secret, tokenStr)
		if err != nil {
			return
		}
		if token.Principal != Admin {
			err = fmt.Errorf("Not admin")
			return
		}
		return f(c)
	})
}

func (self *Server) Handle(r *mux.Route, f func(c *HTTPContext) error) {
	r.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{
			ResponseWriter: w,
			request:        r,
			start:          time.Now(),
			status:         200,
			web:            self,
		}
		for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
			if strings.TrimSpace(encoding) == "gzip" {
				rw.gzipWriter = gzip.NewWriter(rw.ResponseWriter)
				rw.ResponseWriter.Header().Set("Content-Encoding", "gzip")
				defer rw.Close()
				break
			}
		}
		var i int64
		defer func() {
			atomic.StoreInt64(&i, 1)
			rw.log(recover())
		}()
		go func() {
			time.Sleep(time.Second)
			if atomic.CompareAndSwapInt64(&i, 0, 1) {
				rw.inc()
			}
		}()
		if err := f(self.GetContext(rw, r)); err != nil {
			rw.WriteHeader(500)
			fmt.Fprintln(rw, err)
			self.Errorf("%v", err)
		}
		return
	})
}

func (self *Server) Logf(level int, format string, args ...interface{}) {
	if level <= self.logLevel {
		log.Printf(format, args...)
	}
}

func (self *Server) Errlog(err error) {
	if err != nil {
		self.Errorf("%v", err)
	}
}

func (self *Server) Fatalf(format string, args ...interface{}) {
	self.Logf(Fatal, "\033[1;31mFATAL\t"+format+"\033[0m", args...)
}

func (self *Server) Errorf(format string, args ...interface{}) {
	self.Logf(Error, "\033[31mERROR\t"+format+"\033[0m", args...)
}

func (self *Server) Infof(format string, args ...interface{}) {
	self.Logf(Info, "INFO\t"+format, args...)
}

func (self *Server) Debugf(format string, args ...interface{}) {
	self.Logf(Debug, "\033[32mDEBUG\t"+format+"\033[0m", args...)
}

func (self *Server) Tracef(format string, args ...interface{}) {
	self.Logf(Trace, "\033[1;32mTRACE\t"+format+"\033[0m", args...)
}

func (self *Server) OrderedAllocationMethods() (result string, err error) {
	b, err := json.Marshal(allocation.OrderedMethods)
	if err != nil {
		return
	}
	result = string(b)
	return
}

func (self *Server) OrderedVariants() (result string, err error) {
	b, err := json.Marshal(variants.OrderedVariants)
	if err != nil {
		return
	}
	result = string(b)
	return
}

func (self *Server) Variants() (result string, err error) {
	b, err := json.Marshal(variants.Variants)
	if err != nil {
		return
	}
	result = string(b)
	return
}

func (self *Server) GameStates() (result string, err error) {
	b, err := json.Marshal(meta.GameStates)
	if err != nil {
		return
	}
	result = string(b)
	return
}

func (self *Server) HandleStatic(router *mux.Router, dir string) (err error) {
	full := filepath.Join(".", dir)
	legal, err := filepath.Abs(full)
	if err != nil {
		self.Errorf("Trying to filepath.Abs(%#v): %v", full, err)
		return
	}
	self.Handle(router.MatcherFunc(func(r *http.Request, rm *mux.RouteMatch) bool {
		full := filepath.Join(".", r.URL.Path)
		abs, err := filepath.Abs(full)
		if err != nil {
			self.Errorf("Trying to filepath.Abs(%#v): %v", full, err)
			return false
		}
		if !strings.HasPrefix(abs, legal) {
			return false
		}
		if stat, err := os.Stat(abs); err != nil {
			if !os.IsNotExist(err) {
				self.Errorf("Trying to stat %#v: %v", abs, err)
			}
			return false
		} else if stat.IsDir() {
			return false
		}
		return true
	}), func(c *HTTPContext) (err error) {
		full := filepath.Join(".", c.Req().URL.Path)
		abs, err := filepath.Abs(full)
		if err != nil {
			return
		}
		if strings.HasSuffix(abs, ".css") {
			c.SetContentType("text/css; charset=UTF-8")
		} else if strings.HasSuffix(abs, ".png") {
			c.SetContentType("image/png")
		} else if strings.HasSuffix(abs, ".gif") {
			c.SetContentType("image/gif")
		} else if strings.HasSuffix(abs, ".html") {
			c.SetContentType("text/html; charset=UTF-8")
		} else if strings.HasSuffix(abs, ".js") {
			c.SetContentType("application/javascript; charset=UTF-8")
		} else if strings.HasSuffix(abs, ".ttf") {
			c.SetContentType("font/truetype")
		} else {
			c.SetContentType("application/octet-stream")
		}
		b, err := ioutil.ReadFile(abs)
		if err != nil {
			return
		}
		if _, err = c.Resp().Write(b); err != nil {
			return
		}
		return
	})
	return
}

type skinnyServer struct {
	*Server
}

func (self *skinnyServer) AfterTransaction(f func(SkinnyContext) error) (err error) {
	return self.Server.db.AfterTransaction(func(d *unbolted.DB) (err error) {
		return f(self)
	})
}

func (self *skinnyServer) View(f func(c SkinnyTXContext) error) error {
	return self.Server.db.View(func(tx *unbolted.TX) error {
		return f(&skinnyTXContext{
			SkinnyContext: self,
			tx:            tx,
		})
	})
}

func (self *skinnyServer) Update(f func(c SkinnyTXContext) error) error {
	return self.Server.db.Update(func(tx *unbolted.TX) error {
		return f(&skinnyTXContext{
			SkinnyContext: self,
			tx:            tx,
		})
	})
}

func (self *skinnyServer) DB() *unbolted.DB {
	return self.Server.db
}