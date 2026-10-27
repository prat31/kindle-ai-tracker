package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/device"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
	"github.com/prat31/kindle-ai-tracker/internal/render"
)

var errCancel = errors.New("cancelled")

const loginTimeout = 20 * time.Minute

type submission struct {
	input string
	reply chan error
}

// login connects one provider. Paste logins show a sign-in QR and a QR for a phone page
// where the code or key is pasted (or it can be saved as code.txt over USB); device-code
// logins show the code to type on the service's site and poll until it's approved.
func (a *App) login(src *source) error {
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	status := "Starting…"
	var prompt provider.LoginPrompt
	show := func(paste string) {
		hint := strings.TrimPrefix(filepath.Join(src.dir, "code.txt"), "/mnt/us/")
		a.display.Show(render.Login(a.view(src), prompt, paste, hint, status).Img, true)
	}
	begin := func() error {
		var err error
		bctx, bcancel := context.WithTimeout(ctx, 30*time.Second)
		defer bcancel()
		prompt, err = src.p.BeginLogin(bctx)
		return err
	}
	if err := begin(); err != nil {
		return err
	}
	if prompt.UserCode != "" {
		return a.pollLogin(ctx, src, &prompt, begin, show, &status)
	}

	subs := make(chan submission)
	srv := &http.Server{Handler: pastePage(src.p.Name(), prompt.InputLabel, subs), ReadHeaderTimeout: 10 * time.Second}
	if ln, err := net.Listen("tcp", ":"+strconv.Itoa(a.cfg.LoginPort)); err != nil {
		log.Printf("paste page unavailable: %v", err)
	} else {
		device.Firewall(true, a.cfg.LoginPort)
		go srv.Serve(ln)
		defer func() {
			srv.Close()
			device.Firewall(false, a.cfg.LoginPort)
		}()
	}

	codeFile := filepath.Join(src.dir, "code.txt")
	status = "Waiting for " + prompt.InputLabel + "…"
	ip, shownIP := "", "-"
	paste := func() string {
		if ip == "" {
			return ""
		}
		return fmt.Sprintf("http://%s:%d", ip, a.cfg.LoginPort)
	}
	try := func(input string) error {
		status = "Checking…"
		show(paste())
		cctx, ccancel := context.WithTimeout(ctx, 45*time.Second)
		defer ccancel()
		if err := src.p.CompleteLogin(cctx, input); err != nil {
			log.Printf("%s login failed: %v", src.p.ID(), err)
			status = "That didn't work: " + err.Error() + " — try again"
			shownIP = "-"
			return err
		}
		return nil
	}

	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		if ip != shownIP {
			show(paste())
			shownIP = ip
		}
		select {
		case <-ctx.Done():
			return errCancel
		case <-a.touch.Hold:
			if a.loginMenu() {
				return errCancel
			}
			shownIP = "-" // redraw the login screen
		case <-a.touch.Tap:
		case s := <-subs:
			err := try(s.input)
			s.reply <- err
			if err == nil {
				return nil
			}
		case <-tick.C:
			ip = device.WifiIP()
			if b, err := os.ReadFile(codeFile); err == nil {
				os.Remove(codeFile)
				if try(string(b)) == nil {
					return nil
				}
			}
		}
	}
}

// pollLogin waits for a device code to be approved, starting over with a fresh code
// if one expires or is denied.
func (a *App) pollLogin(ctx context.Context, src *source, prompt *provider.LoginPrompt,
	begin func() error, show func(string), status *string) error {
	poller, ok := src.p.(provider.Poller)
	if !ok {
		return errors.New(src.p.Name() + " gave a device code but can't be polled")
	}
	*status = "Waiting for you to approve on " + render.Host(prompt.SignInURL) + "…"
	show("")
	tick := time.NewTicker(max(prompt.PollEvery, 3*time.Second))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return errCancel
		case <-a.touch.Hold:
			if a.loginMenu() {
				return errCancel
			}
			show("")
		case <-a.touch.Tap:
		case <-tick.C:
			pctx, pcancel := context.WithTimeout(ctx, 30*time.Second)
			done, err := poller.PollLogin(pctx)
			pcancel()
			if done {
				return nil
			}
			if err == nil {
				continue
			}
			log.Printf("%s device login: %v", src.p.ID(), err)
			var ne net.Error
			if errors.As(err, &ne) {
				continue // a network hiccup; keep polling
			}
			if err := begin(); err != nil {
				return err
			}
			*status = "That code stopped working, here's a new one"
			show("")
		}
	}
}

const pageStyle = `<meta name="viewport" content="width=device-width,initial-scale=1">
<style>body{font-family:-apple-system,system-ui,sans-serif;max-width:560px;margin:40px auto;padding:0 20px;line-height:1.45}
textarea{width:100%;height:120px;font-size:16px;padding:10px;box-sizing:border-box}
button{font-size:18px;padding:12px 24px;margin-top:12px;background:#111;color:#fff;border:0;border-radius:8px}
.err{color:#b00020}.ok{color:#0a7d27}</style>`

func pastePage(name, inputLabel string, subs chan<- submission) http.Handler {
	mux := http.NewServeMux()
	form := func(w http.ResponseWriter, msg string) {
		fmt.Fprintf(w, `<!doctype html><title>Kindle AI dashboard</title>%s
<h2>Connect %s to your Kindle</h2>
<p>Paste %s below.</p>%s
<form method="post" action="/submit"><textarea name="input" autofocus></textarea>
<button type="submit">Send to Kindle</button></form>`, pageStyle, html.EscapeString(name), html.EscapeString(inputLabel), msg)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { form(w, "") })
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		input := strings.TrimSpace(r.FormValue("input"))
		if r.Method != http.MethodPost || input == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		reply := make(chan error, 1)
		select {
		case subs <- submission{input, reply}:
		case <-time.After(10 * time.Second):
			form(w, `<p class="err">The Kindle is busy, try again.</p>`)
			return
		}
		select {
		case err := <-reply:
			if err != nil {
				form(w, `<p class="err">`+html.EscapeString(err.Error())+`</p>`)
				return
			}
			fmt.Fprintf(w, `<!doctype html>%s<h2 class="ok">Connected ✓</h2><p>Your Kindle will show your %s usage in a moment. You can close this page.</p>`, pageStyle, html.EscapeString(name))
		case <-time.After(60 * time.Second):
			form(w, `<p class="err">No answer yet; check the Kindle screen.</p>`)
		}
	})
	return mux
}
