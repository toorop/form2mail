package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-gomail/gomail"
	"github.com/spf13/viper"
	"github.com/tomasen/realip"
)

var rates = struct {
	sync.RWMutex
	m map[int64]string
}{m: make(map[int64]string)}

func main() {
	// config
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatal("unable to read config")
	}

	// clean rates
	go func() {
		for {
			removeBefore := time.Now().Add(-1 * time.Hour)
			rates.Lock()
			for ts := range rates.m {
				if time.Unix(0, ts).Before(removeBefore) {
					delete(rates.m, ts)
				}
			}
			rates.Unlock()
			time.Sleep(60 * time.Second)
		}
	}()

	// http
	http.HandleFunc("/", Form2Mail)
	log.Fatal(http.ListenAndServe(":3615", nil))
}

const mailBody = `%s

-- 
%s
%s
%s
`

// Form2Mail main handler
func Form2Mail(w http.ResponseWriter, req *http.Request) {
	// TODO recover

	clientIP := realip.FromRequest(req)
	log.Println(clientIP)

	// method must be POST
	if req.Method != http.MethodPost {
		log.Println("bad method")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// naive rates limitation
	var emailsSent int

	rates.RLock()
	for _, ip := range rates.m {
		if ip == clientIP {
			log.Println(clientIP)
			emailsSent = emailsSent + 1
			if emailsSent > viper.GetInt("ratePerIpPerHour") {
				log.Printf("rate limiting for %v", clientIP)
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
		}
	}
	rates.RUnlock()

	// get RCPT TO -> last part of URI
	p := strings.Split(req.URL.String(), "/")
	if len(p) != 2 {
		log.Println("no rcpt to found")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	rcptTo := strings.ToLower(strings.TrimSpace(p[1]))
	// is a valid rcpt to
	isValid := false
	for _, r := range viper.GetStringSlice("validRecipents") {
		if rcptTo == strings.ToLower(r) {
			isValid = true
			break
		}
	}
	if !isValid {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// parse form
	if err := req.ParseForm(); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// check form
	for _, k := range []string{"message", "name", "email", "phone"} {
		_, ok := req.Form[k]
		if !ok {
			log.Printf("%s bad form %s is missing", clientIP, k)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

	}

	// send mail
	body := fmt.Sprintf(mailBody, req.Form["message"][0], req.Form["name"][0], req.Form["email"][0], req.Form["phone"][0])
	subject := fmt.Sprintf("New message from: %s", viper.GetString("siteName"))

	m := gomail.NewMessage()
	m.SetHeader("From", viper.GetString("smtp.sender"))
	m.SetHeader("To", rcptTo)
	m.SetHeader("Reply-to", req.Form["email"][0])
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	d := gomail.NewDialer(viper.GetString("smtp.host"), viper.GetInt("smtp.port"), viper.GetString("smtp.user"), viper.GetString("smtp.password"))

	if err := d.DialAndSend(m); err != nil {
		log.Printf("send mail failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// rates
	rates.Lock()
	rates.m[time.Now().UnixNano()] = clientIP
	rates.Unlock()

	// ok
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
}
