package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

const hubURL = "https://pubsubhubbub.appspot.com/"

func main() {
	port := env("PORT", "8080")
	publicBase := mustEnv("PUBLIC_BASE_URL") // e.g. https://abcd.ngrok.io or https://your.domain.tld
	channelID := mustEnv("YOUTUBE_CHANNEL_ID")
	callback := publicBase + "/websub"
	verifyToken := env("VERIFY_TOKEN", "devtoken")

	// HTTP: GET=verify, POST=notify
	http.HandleFunc("/websub", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Verification: echo hub.challenge
			q := r.URL.Query()
			mode := q.Get("hub.mode")
			topic := q.Get("hub.topic")
			challenge := q.Get("hub.challenge")
			lease := q.Get("hub.lease_seconds")
			vt := q.Get("hub.verify_token")
			log.Printf("[VERIFY] mode=%s topic=%s lease_seconds=%s verify_token=%s", mode, topic, lease, vt)

			// Optional: check verify_token matches what we sent
			if vt != "" && vt != verifyToken {
				http.Error(w, "bad verify_token", http.StatusForbidden)
				return
			}
			// MUST return the challenge exactly
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(challenge))
		case http.MethodPost:
			// Notification: log the Atom feed that YouTube posts
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			log.Printf("[NOTIFY] content-type=%s bytes=%d", r.Header.Get("Content-Type"), len(body))
			// For a quick peek, log first 1KB (avoid massive logs)
			max := 1024
			if len(body) < max {
				max = len(body)
			}
			log.Printf("[NOTIFY] snippet:\n%s", string(body[:max]))
			// Respond 204 per spec/best practice
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Subscribe now, then periodically renew (simple timer)
	go func() {
		topic := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?channel_id=%s", channelID)
		for {
			if err := subscribe(callback, topic, verifyToken); err != nil {
				log.Printf("subscribe error: %v", err)
			} else {
				log.Printf("subscribe OK for topic=%s", topic)
			}
			// Lease seconds vary; simplest: renew daily.
			time.Sleep(24 * time.Hour)
		}
	}()

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func subscribe(callback, topic, verifyToken string) error {
	values := url.Values{}
	values.Set("hub.mode", "subscribe")
	values.Set("hub.topic", topic)
	values.Set("hub.callback", callback)
	values.Set("hub.verify", "async")
	values.Set("hub.verify_token", verifyToken)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, hubURL+"subscribe", bytes.NewBufferString(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("hub subscribe status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env %s", key)
	}
	return v
}

func env(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
