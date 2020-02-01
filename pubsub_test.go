package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestPubSub(t *testing.T) {
	ps := NewPubSub()
	defer ps.Close()

	srv, wsURL, err := newTestHTTP(t, ps)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	t.Run("publish, no subscribers", func(t *testing.T) {
		// Ensure it doesn't crash or hang w no subs
		w := httptest.NewRecorder()
		ps.Publish(w, newTestPublishReq("test1"))
		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("failed with: %s", resp.Status)
		}
	})

	wg := &sync.WaitGroup{}

	doSubscriber := func(n int, m map[string]int) {
		c, _, err := newTestSubscribeWS(wsURL)
		if err != nil {
			t.Error(err)
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close(websocket.StatusNormalClosure, "defer")
				for {
					_, bytes, err := c.Read(context.Background())
					if err != nil {
						if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
							c.Close(websocket.StatusNormalClosure, "ok")
							return
						}
						t.Error(err)
						return
					}
					var x struct {
						Data string `json:"data"`
					}
					err = json.Unmarshal(bytes, &x)
					if err != nil {
						t.Error(err)
						return
					}
					//t.Logf("#%d Read: %s", n, x.Data)
					m[x.Data]++
				}
			}()
		}
	}

	// First subscriber:
	m1 := map[string]int{}
	doSubscriber(1, m1)

	// Second subscriber:
	m2 := map[string]int{}
	doSubscriber(2, m2)

	// Publish stuff:
	mpub := map[string]int{}
	for i := 0; i < 1000; i++ {
		data := fmt.Sprintf("data#%d", i)
		w := httptest.NewRecorder()
		ps.Publish(w, newTestPublishReq(data))
		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("failed with: %s", resp.Status)
		}
		mpub[data]++
	}

	// Wait for subscribers to get everything.
	// TODO: improve this.
	time.Sleep(500 * time.Millisecond)

	ps.Close()
	wg.Wait()

	// Check that the subscribers got everything.
	for data, n := range mpub {
		if m1[data] != n {
			t.Errorf("m1 does not contain %d copies of %s", n, data)
			break
		}
	}
	for data, n := range mpub {
		if m2[data] != n {
			t.Errorf("m2 does not contain %d copies of %s", n, data)
			break
		}
	}

}

func newTestHTTP(t *testing.T, ps *PubSub) (*http.Server, string, error) {
	// Using actual server get to the websocket:
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, "", err
	}
	mux := &http.ServeMux{}
	mux.HandleFunc("/publish", ps.Publish)
	mux.HandleFunc("/subscribe", ps.Subscribe)
	srv := &http.Server{
		Handler: mux,
	}
	go func() {
		err := srv.Serve(l)
		if err != http.ErrServerClosed {
			t.Fatalf("failed to listen and serve: %v", err)
		}
	}()
	return srv, "ws://" + l.Addr().String(), nil
}

// Run with -race
func TestPubSubRace(t *testing.T) {
	ps := NewPubSub()
	defer ps.Close()

	srv, _, err := newTestHTTP(t, ps)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	wg := &sync.WaitGroup{}

	/*// Open a misbehaving subscriber.
	c, _, err := newTestSubscribeWS(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer c.Close(websocket.StatusNormalClosure, "rage quit")
		time.Sleep(200 * time.Millisecond)
	}() //*/

	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				data := fmt.Sprintf("go n=%d #%d", n, i)
				w := httptest.NewRecorder()
				ps.Publish(w, newTestPublishReq(data))
				resp := w.Result()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("failed with: %s", resp.Status)
				}
			}
		}(n)
	}

	wg.Wait()
}

func newTestSubscribeWS(wsURL string) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(context.Background(), wsURL+"/subscribe", nil)
}

func newTestPublishReq(data string) *http.Request {
	qs := url.Values{}
	qs.Set("data", data)
	return httptest.NewRequest("GET", "/publish?"+qs.Encode(), nil)
}
